// Package web 是後台的 HTTP 層：server-rendered 頁面 + htmx 局部更新。
package web

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/jacobintern/TheShop/admin/internal/backend"
	"github.com/jacobintern/TheShop/admin/internal/config"
	"github.com/jacobintern/TheShop/admin/internal/store"
)

//go:embed templates/* templates/*/* static/*
var assets embed.FS

const (
	sessionCookie = "admin_session"
	ctxUserKey    = "adminUser"
	defaultFetch  = 20
)

// Backend 是 handler 用到的 backend 操作，抽 interface 方便測試。
type Backend interface {
	ListOrders(ctx context.Context, page, fetch int, status string) (backend.Page[backend.Order], error)
	GetOrder(ctx context.Context, id int) (backend.Order, error)
	UpdateOrderStatus(ctx context.Context, id int, status string) (backend.Order, error)
	ListProducts(ctx context.Context, page, fetch int) (backend.Page[backend.Product], error)
	GetProduct(ctx context.Context, id int) (backend.Product, error)
	CreateProduct(ctx context.Context, in backend.ProductInput) (int, error)
	UpdateProduct(ctx context.Context, id int, in backend.ProductInput) (backend.Product, error)
	SetProductStatus(ctx context.Context, id, status int) error
}

type Server struct {
	cfg     config.Config
	store   *store.Store
	backend Backend
	engine  *gin.Engine
	pages   map[string]*template.Template
}

func New(cfg config.Config, st *store.Store, be Backend) (*Server, error) {
	s := &Server{cfg: cfg, store: st, backend: be, pages: map[string]*template.Template{}}
	if err := s.loadTemplates(); err != nil {
		return nil, err
	}
	s.engine = gin.New()
	s.engine.Use(gin.Logger(), gin.Recovery())
	s.routes()
	return s, nil
}

func (s *Server) Handler() http.Handler { return s.engine }

func (s *Server) routes() {
	r := s.engine

	staticFS, _ := fs.Sub(assets, "static")
	r.StaticFS("/static", http.FS(staticFS))
	if st, err := os.Stat(s.cfg.AssetDir); err == nil && st.IsDir() {
		r.Static("/asset", s.cfg.AssetDir)
	}

	r.GET("/healthz", func(c *gin.Context) { c.String(http.StatusOK, "ok") })

	r.GET("/login", s.loginPage)
	r.POST("/login", s.login)
	r.POST("/logout", s.logout)

	auth := r.Group("/", s.requireLogin)
	auth.GET("/", func(c *gin.Context) { c.Redirect(http.StatusFound, "/orders") })

	auth.GET("/orders", s.orderList)
	auth.GET("/orders/:id", s.orderDetail)
	auth.POST("/orders/:id/status", s.orderStatus)

	auth.GET("/products", s.productList)
	auth.GET("/products/new", s.productNew)
	auth.POST("/products", s.productCreate)
	auth.GET("/products/:id/edit", s.productEdit)
	auth.POST("/products/:id", s.productUpdate)
	auth.POST("/products/:id/status", s.productStatus)

	auth.GET("/accounts", s.accountList)
	auth.POST("/accounts", s.accountCreate)
	auth.POST("/accounts/:id/delete", s.accountDelete)
	auth.POST("/accounts/password", s.accountPassword)
}

// ---- templates ----

func (s *Server) loadTemplates() error {
	funcs := template.FuncMap{
		"money": func(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) },
		"seq": func(n int) []int {
			out := make([]int, n)
			for i := range out {
				out[i] = i + 1
			}
			return out
		},
		"statusLabel":  orderStatusLabel,
		"nextStatuses": nextOrderStatuses,
		"add":          func(a, b int) int { return a + b },
		"hasPrefix":    strings.HasPrefix,
		"dict": func(kv ...any) (map[string]any, error) {
			if len(kv)%2 != 0 {
				return nil, errors.New("dict: odd number of arguments")
			}
			m := make(map[string]any, len(kv)/2)
			for i := 0; i < len(kv); i += 2 {
				k, ok := kv[i].(string)
				if !ok {
					return nil, errors.New("dict: key must be string")
				}
				m[k] = kv[i+1]
			}
			return m, nil
		},
	}
	pages, err := fs.Glob(assets, "templates/pages/*.html")
	if err != nil {
		return err
	}
	for _, p := range pages {
		name := strings.TrimSuffix(strings.TrimPrefix(p, "templates/pages/"), ".html")
		t, err := template.New("layout.html").Funcs(funcs).
			ParseFS(assets, "templates/layout.html", "templates/partials/*.html", p)
		if err != nil {
			return fmt.Errorf("parse template %s: %w", p, err)
		}
		s.pages[name] = t
	}
	return nil
}

func (s *Server) render(c *gin.Context, status int, page string, data gin.H) {
	t, ok := s.pages[page]
	if !ok {
		c.String(http.StatusInternalServerError, "template %s not found", page)
		return
	}
	if data == nil {
		data = gin.H{}
	}
	if u, ok := c.Get(ctxUserKey); ok {
		data["User"] = u
	}
	data["Active"] = page
	if _, ok := data["Msg"]; !ok {
		data["Msg"] = c.Query("msg")
	}
	if _, ok := data["Err"]; !ok {
		data["Err"] = c.Query("err")
	}
	c.Status(status)
	c.Header("Content-Type", "text/html; charset=utf-8")
	if err := t.ExecuteTemplate(c.Writer, "layout.html", data); err != nil {
		_ = c.Error(err)
	}
}

// renderPartial 給 htmx 用，只輸出一個 partial template。
func (s *Server) renderPartial(c *gin.Context, status int, partial string, data gin.H) {
	t := s.pages["order_detail"]
	c.Status(status)
	c.Header("Content-Type", "text/html; charset=utf-8")
	if err := t.ExecuteTemplate(c.Writer, partial, data); err != nil {
		_ = c.Error(err)
	}
}

func isHTMX(c *gin.Context) bool { return c.GetHeader("HX-Request") == "true" }

// redirectMsg 用 query string 帶一次性訊息，MVP 不做 flash session。
func redirectMsg(c *gin.Context, path, msg, errMsg string) {
	q := url.Values{}
	if msg != "" {
		q.Set("msg", msg)
	}
	if errMsg != "" {
		q.Set("err", errMsg)
	}
	if len(q) > 0 {
		sep := "?"
		if strings.Contains(path, "?") {
			sep = "&"
		}
		path += sep + q.Encode()
	}
	c.Redirect(http.StatusSeeOther, path)
}

// ---- auth ----

func (s *Server) requireLogin(c *gin.Context) {
	token, err := c.Cookie(sessionCookie)
	if err != nil || token == "" {
		s.rejectUnauthenticated(c)
		return
	}
	acc, err := s.store.GetSession(c.Request.Context(), token)
	if err != nil {
		s.clearSessionCookie(c)
		s.rejectUnauthenticated(c)
		return
	}
	c.Set(ctxUserKey, acc)
	c.Next()
}

func (s *Server) rejectUnauthenticated(c *gin.Context) {
	if isHTMX(c) {
		c.Header("HX-Redirect", "/login")
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	}
	c.Redirect(http.StatusFound, "/login")
	c.Abort()
}

func (s *Server) setSessionCookie(c *gin.Context, token string) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		MaxAge:   int(s.cfg.SessionTTL / time.Second),
		HttpOnly: true,
		Secure:   s.cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *Server) clearSessionCookie(c *gin.Context) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name: sessionCookie, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: s.cfg.CookieSecure, SameSite: http.SameSiteLaxMode,
	})
}

func currentUser(c *gin.Context) *store.Account {
	u, _ := c.Get(ctxUserKey)
	acc, _ := u.(*store.Account)
	return acc
}

func (s *Server) loginPage(c *gin.Context) {
	if token, err := c.Cookie(sessionCookie); err == nil && token != "" {
		if _, err := s.store.GetSession(c.Request.Context(), token); err == nil {
			c.Redirect(http.StatusFound, "/orders")
			return
		}
	}
	s.render(c, http.StatusOK, "login", nil)
}

func (s *Server) login(c *gin.Context) {
	account := strings.TrimSpace(c.PostForm("account"))
	password := c.PostForm("password")
	acc, err := s.store.Authenticate(c.Request.Context(), account, password)
	if err != nil {
		if errors.Is(err, store.ErrInvalidCredential) {
			s.render(c, http.StatusUnauthorized, "login", gin.H{"Err": "帳號或密碼錯誤", "Account": account})
			return
		}
		s.render(c, http.StatusInternalServerError, "login", gin.H{"Err": "登入失敗：" + err.Error()})
		return
	}
	token, err := s.store.CreateSession(c.Request.Context(), acc.ID, s.cfg.SessionTTL)
	if err != nil {
		s.render(c, http.StatusInternalServerError, "login", gin.H{"Err": "建立 session 失敗：" + err.Error()})
		return
	}
	s.setSessionCookie(c, token)
	c.Redirect(http.StatusSeeOther, "/orders")
}

func (s *Server) logout(c *gin.Context) {
	if token, err := c.Cookie(sessionCookie); err == nil && token != "" {
		_ = s.store.DeleteSession(c.Request.Context(), token)
	}
	s.clearSessionCookie(c)
	c.Redirect(http.StatusSeeOther, "/login")
}

// ---- helpers ----

func pageParam(c *gin.Context) int {
	p, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	if p < 1 {
		p = 1
	}
	return p
}

func idParam(c *gin.Context) (int, bool) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

// backendErrMsg 把 backend 錯誤轉成給管理員看的中文。
func backendErrMsg(err error) string {
	var be *backend.Error
	if errors.As(err, &be) {
		if be.HTTPStatus == http.StatusNotFound && be.Code != backend.CodeOrderNotFound && be.Code != backend.CodeProductNotFound {
			return "backend 找不到這支 API，/api/admin/* 可能尚未部署"
		}
		if be.Code == 0 {
			return fmt.Sprintf("backend 回 HTTP %d：%s", be.HTTPStatus, be.Msg)
		}
		switch be.Code {
		case backend.CodeInvalidAdminKey:
			return "backend 拒絕管理金鑰，請檢查 ADMIN_API_KEY 設定"
		case backend.CodeOrderNotFound:
			return "找不到訂單"
		case backend.CodeProductNotFound:
			return "找不到商品"
		case backend.CodeOrderStatusNotAllowed:
			return "目前訂單狀態不允許這個操作"
		case backend.CodeInvalidArgument:
			return "backend 回報參數不合法：" + be.Msg
		}
		return fmt.Sprintf("backend 錯誤（%d）：%s", be.Code, be.Msg)
	}
	return "無法連線 backend：" + err.Error()
}

var orderStatuses = []string{"pending", "paid", "shipped", "completed", "cancelled"}

func orderStatusLabel(status string) string {
	switch status {
	case "pending":
		return "待付款"
	case "paid":
		return "已付款"
	case "shipped":
		return "已出貨"
	case "completed":
		return "已完成"
	case "cancelled":
		return "已取消"
	}
	return status
}

// nextOrderStatuses 對齊 backend entity.Order 的狀態機；backend 仍是最終裁判。
func nextOrderStatuses(status string) []string {
	switch status {
	case "pending":
		return []string{"paid", "cancelled"}
	case "paid":
		return []string{"shipped"}
	case "shipped":
		return []string{"completed"}
	}
	return nil
}

func validOrderStatus(status string) bool {
	for _, s := range orderStatuses {
		if s == status {
			return true
		}
	}
	return false
}
