package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jacobintern/TheShop/admin/internal/backend"
	"github.com/jacobintern/TheShop/admin/internal/config"
	"github.com/jacobintern/TheShop/admin/internal/store"
)

type fakeBackend struct {
	orders   map[int]backend.Order
	products map[int]backend.Product
	statusCh []string
}

func (f *fakeBackend) ListOrders(_ context.Context, _, _ int, status string) (backend.Page[backend.Order], error) {
	var p backend.Page[backend.Order]
	for _, o := range f.orders {
		if status == "" || o.Status == status {
			p.List = append(p.List, o)
		}
	}
	return p, nil
}
func (f *fakeBackend) GetOrder(_ context.Context, id int) (backend.Order, error) {
	o, ok := f.orders[id]
	if !ok {
		return o, &backend.Error{HTTPStatus: 400, Code: backend.CodeOrderNotFound, Msg: "order not found."}
	}
	return o, nil
}
func (f *fakeBackend) UpdateOrderStatus(ctx context.Context, id int, status string) (backend.Order, error) {
	o, err := f.GetOrder(ctx, id)
	if err != nil {
		return o, err
	}
	if !contains(nextOrderStatuses(o.Status), status) {
		return o, &backend.Error{HTTPStatus: 400, Code: backend.CodeOrderStatusNotAllowed, Msg: "order status does not allow this operation."}
	}
	o.Status = status
	f.orders[id] = o
	f.statusCh = append(f.statusCh, status)
	return o, nil
}
func (f *fakeBackend) ListProducts(context.Context, int, int) (backend.Page[backend.Product], error) {
	var p backend.Page[backend.Product]
	for _, pr := range f.products {
		p.List = append(p.List, pr)
	}
	return p, nil
}
func (f *fakeBackend) GetProduct(_ context.Context, id int) (backend.Product, error) {
	p, ok := f.products[id]
	if !ok {
		return p, &backend.Error{HTTPStatus: 400, Code: backend.CodeProductNotFound, Msg: "product not found."}
	}
	return p, nil
}
func (f *fakeBackend) CreateProduct(_ context.Context, in backend.ProductInput) (int, error) {
	id := len(f.products) + 1
	f.products[id] = backend.Product{ID: id, Name: in.Name, Price: in.Price, Stock: in.Stock, Spec: in.Spec, Status: 1, Images: in.Images}
	return id, nil
}
func (f *fakeBackend) UpdateProduct(_ context.Context, id int, in backend.ProductInput) (backend.Product, error) {
	p := f.products[id]
	p.Name, p.Price, p.Stock, p.Spec, p.Images = in.Name, in.Price, in.Stock, in.Spec, in.Images
	f.products[id] = p
	return p, nil
}
func (f *fakeBackend) SetProductStatus(_ context.Context, id, status int) error {
	p := f.products[id]
	p.Status = status
	f.products[id] = p
	return nil
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

type testEnv struct {
	srv  *httptest.Server
	be   *fakeBackend
	jar  []*http.Cookie
	http *http.Client
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	gin.SetMode(gin.TestMode)
	st, err := store.Open(filepath.Join(t.TempDir(), "admin.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })
	_, err = st.CreateAccount(context.Background(), "admin", "password123")
	require.NoError(t, err)

	be := &fakeBackend{
		orders: map[int]backend.Order{
			1: {ID: 1, AccountID: 9, Total: 120, Status: "pending", Items: []backend.OrderItem{{ProductID: 1, Num: 2, SubTotal: 120}}},
			2: {ID: 2, AccountID: 9, Total: 60, Status: "paid"},
		},
		products: map[int]backend.Product{
			1: {ID: 1, Name: "牛奶", Price: 60, Stock: 10, Status: 1, Images: []backend.ProductImage{{Type: 0, URL: "http://img/banner.png"}, {Type: 1, URL: "http://img/1.png"}}},
		},
	}
	cfg := config.Config{SessionTTL: time.Hour, AssetDir: t.TempDir()}
	s, err := New(cfg, st, be)
	require.NoError(t, err)
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)

	env := &testEnv{srv: srv, be: be}
	env.http = &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return env
}

func (e *testEnv) do(t *testing.T, method, path string, form url.Values, headers map[string]string) (*http.Response, string) {
	t.Helper()
	var body *strings.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	} else {
		body = strings.NewReader("")
	}
	req, err := http.NewRequest(method, e.srv.URL+path, body)
	require.NoError(t, err)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	for _, c := range e.jar {
		req.AddCookie(c)
	}
	resp, err := e.http.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	if cs := resp.Cookies(); len(cs) > 0 {
		e.jar = cs
	}
	buf := new(strings.Builder)
	_, _ = copyBody(buf, resp)
	return resp, buf.String()
}

func (e *testEnv) login(t *testing.T) {
	t.Helper()
	resp, _ := e.do(t, http.MethodPost, "/login", url.Values{"account": {"admin"}, "password": {"password123"}}, nil)
	require.Equal(t, http.StatusSeeOther, resp.StatusCode)
	require.Equal(t, "/orders", resp.Header.Get("Location"))
}

func TestLoginRequired(t *testing.T) {
	env := newTestEnv(t)
	resp, _ := env.do(t, http.MethodGet, "/orders", nil, nil)
	assert.Equal(t, http.StatusFound, resp.StatusCode)
	assert.Equal(t, "/login", resp.Header.Get("Location"))

	resp, _ = env.do(t, http.MethodGet, "/orders", nil, map[string]string{"HX-Request": "true"})
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	assert.Equal(t, "/login", resp.Header.Get("HX-Redirect"))
}

func TestLoginWrongPassword(t *testing.T) {
	env := newTestEnv(t)
	resp, body := env.do(t, http.MethodPost, "/login", url.Values{"account": {"admin"}, "password": {"nope"}}, nil)
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	assert.Contains(t, body, "帳號或密碼錯誤")
}

func TestOrderPagesRender(t *testing.T) {
	env := newTestEnv(t)
	env.login(t)

	resp, body := env.do(t, http.MethodGet, "/orders?status=paid", nil, nil)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, body, "#2")
	assert.NotContains(t, body, "#1</td>")

	resp, body = env.do(t, http.MethodGet, "/orders/1", nil, nil)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, body, "訂單 #1")
	assert.Contains(t, body, `name="status" value="paid"`)
	assert.Contains(t, body, `name="status" value="cancelled"`)
}

func TestOrderStatusTransition_HTMX(t *testing.T) {
	env := newTestEnv(t)
	env.login(t)

	resp, body := env.do(t, http.MethodPost, "/orders/1/status", url.Values{"status": {"paid"}}, map[string]string{"HX-Request": "true"})
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, body, `id="order-panel"`)
	assert.Contains(t, body, "已付款")
	assert.Contains(t, body, `name="status" value="shipped"`)
	assert.Equal(t, []string{"paid"}, env.be.statusCh)

	resp, body = env.do(t, http.MethodPost, "/orders/1/status", url.Values{"status": {"completed"}}, map[string]string{"HX-Request": "true"})
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, body, "目前訂單狀態不允許這個操作")
}

func TestOrderStatusTransition_NonHTMXRedirects(t *testing.T) {
	env := newTestEnv(t)
	env.login(t)
	resp, _ := env.do(t, http.MethodPost, "/orders/2/status", url.Values{"status": {"shipped"}}, nil)
	assert.Equal(t, http.StatusSeeOther, resp.StatusCode)
	assert.True(t, strings.HasPrefix(resp.Header.Get("Location"), "/orders/2?msg="))
}

func TestProductCreateAndEdit(t *testing.T) {
	env := newTestEnv(t)
	env.login(t)

	resp, body := env.do(t, http.MethodPost, "/products", url.Values{"name": {""}, "price": {"10"}, "stock": {"1"}}, nil)
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	assert.Contains(t, body, "商品名稱必填")

	resp, _ = env.do(t, http.MethodPost, "/products", url.Values{
		"name": {"咖啡"}, "price": {"120.5"}, "stock": {"5"}, "spec": {"中杯"},
		"banner": {"http://img/c.png"}, "images": {"http://img/c1.png\n\nhttp://img/c2.png"},
	}, nil)
	assert.Equal(t, http.StatusSeeOther, resp.StatusCode)
	assert.True(t, strings.HasPrefix(resp.Header.Get("Location"), "/products/2/edit"))
	created := env.be.products[2]
	assert.Equal(t, 120.5, created.Price)
	require.Len(t, created.Images, 3)
	assert.Equal(t, 0, created.Images[0].Type)
	assert.Equal(t, 1, created.Images[2].Type)

	resp, body = env.do(t, http.MethodGet, "/products/1/edit", nil, nil)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, body, `value="http://img/banner.png"`)
	assert.Contains(t, body, "http://img/1.png")

	resp, _ = env.do(t, http.MethodPost, "/products/1", url.Values{"name": {"牛奶"}, "price": {"60"}, "stock": {"10"}, "banner": {""}, "images": {""}}, nil)
	assert.Equal(t, http.StatusSeeOther, resp.StatusCode)
	assert.NotNil(t, env.be.products[1].Images, "清空圖片欄位要送空陣列，不能省略欄位")
	assert.Len(t, env.be.products[1].Images, 0)

	resp, _ = env.do(t, http.MethodPost, "/products/1/status", url.Values{"status": {"0"}, "back": {"/products/1/edit"}}, nil)
	assert.Equal(t, http.StatusSeeOther, resp.StatusCode)
	assert.Equal(t, 0, env.be.products[1].Status)

	resp, body = env.do(t, http.MethodGet, "/products", nil, nil)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, body, "下架")
	assert.Contains(t, body, "咖啡")
}

func TestAccountManagement(t *testing.T) {
	env := newTestEnv(t)
	env.login(t)

	resp, _ := env.do(t, http.MethodPost, "/accounts", url.Values{"account": {"ops"}, "password": {"short"}}, nil)
	assert.Contains(t, resp.Header.Get("Location"), "err=")

	resp, _ = env.do(t, http.MethodPost, "/accounts", url.Values{"account": {"ops"}, "password": {"password456"}}, nil)
	assert.Contains(t, resp.Header.Get("Location"), "msg=")

	resp, body := env.do(t, http.MethodGet, "/accounts", nil, nil)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, body, "ops")
	assert.Contains(t, body, "（我）")

	resp, _ = env.do(t, http.MethodPost, "/accounts/1/delete", nil, nil)
	assert.Contains(t, resp.Header.Get("Location"), "err=")

	resp, _ = env.do(t, http.MethodPost, "/accounts/password", url.Values{
		"old_password": {"password123"}, "new_password": {"password789"}, "confirm_password": {"password789"},
	}, nil)
	assert.Contains(t, resp.Header.Get("Location"), "msg=")

	resp, _ = env.do(t, http.MethodPost, "/logout", nil, nil)
	assert.Equal(t, http.StatusSeeOther, resp.StatusCode)
	resp, _ = env.do(t, http.MethodGet, "/orders", nil, nil)
	assert.Equal(t, http.StatusFound, resp.StatusCode)
}
