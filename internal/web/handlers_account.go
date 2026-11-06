package web

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/jacobintern/TheShop/admin/internal/store"
)

func (s *Server) accountList(c *gin.Context) {
	list, err := s.store.ListAccounts(c.Request.Context())
	data := gin.H{"Accounts": list, "Me": currentUser(c)}
	if err != nil {
		data["Err"] = err.Error()
	}
	s.render(c, http.StatusOK, "accounts", data)
}

func (s *Server) accountCreate(c *gin.Context) {
	account := strings.TrimSpace(c.PostForm("account"))
	password := c.PostForm("password")
	if account == "" || len(password) < 8 {
		redirectMsg(c, "/accounts", "", "帳號必填，密碼至少 8 碼")
		return
	}
	if _, err := s.store.CreateAccount(c.Request.Context(), account, password); err != nil {
		if errors.Is(err, store.ErrAccountExists) {
			redirectMsg(c, "/accounts", "", "帳號已存在")
			return
		}
		redirectMsg(c, "/accounts", "", err.Error())
		return
	}
	redirectMsg(c, "/accounts", "已新增管理員 "+account, "")
}

func (s *Server) accountDelete(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		redirectMsg(c, "/accounts", "", "帳號編號不合法")
		return
	}
	if me := currentUser(c); me != nil && me.ID == id {
		redirectMsg(c, "/accounts", "", "不能刪除自己")
		return
	}
	if err := s.store.DeleteAccount(c.Request.Context(), id); err != nil {
		switch {
		case errors.Is(err, store.ErrLastAccount):
			redirectMsg(c, "/accounts", "", "至少要保留一個管理員")
		case errors.Is(err, store.ErrAccountNotFound):
			redirectMsg(c, "/accounts", "", "找不到帳號")
		default:
			redirectMsg(c, "/accounts", "", err.Error())
		}
		return
	}
	redirectMsg(c, "/accounts", "已刪除管理員", "")
}

func (s *Server) accountPassword(c *gin.Context) {
	me := currentUser(c)
	oldPwd := c.PostForm("old_password")
	newPwd := c.PostForm("new_password")
	if len(newPwd) < 8 {
		redirectMsg(c, "/accounts", "", "新密碼至少 8 碼")
		return
	}
	if newPwd != c.PostForm("confirm_password") {
		redirectMsg(c, "/accounts", "", "兩次輸入的新密碼不一致")
		return
	}
	if err := s.store.UpdatePassword(c.Request.Context(), me.ID, oldPwd, newPwd); err != nil {
		if errors.Is(err, store.ErrInvalidCredential) {
			redirectMsg(c, "/accounts", "", "舊密碼錯誤")
			return
		}
		redirectMsg(c, "/accounts", "", err.Error())
		return
	}
	redirectMsg(c, "/accounts", "密碼已更新", "")
}
