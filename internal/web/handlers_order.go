package web

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func (s *Server) orderList(c *gin.Context) {
	status := c.Query("status")
	if status != "" && !validOrderStatus(status) {
		status = ""
	}
	page := pageParam(c)
	result, err := s.backend.ListOrders(c.Request.Context(), page, defaultFetch, status)
	data := gin.H{
		"Status":   status,
		"Statuses": orderStatuses,
		"Page":     page,
		"Orders":   result.List,
		"HasNext":  len(result.List) >= defaultFetch,
	}
	if err != nil {
		data["Err"] = backendErrMsg(err)
	}
	s.render(c, http.StatusOK, "orders", data)
}

func (s *Server) orderDetail(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		redirectMsg(c, "/orders", "", "訂單編號不合法")
		return
	}
	order, err := s.backend.GetOrder(c.Request.Context(), id)
	if err != nil {
		redirectMsg(c, "/orders", "", backendErrMsg(err))
		return
	}
	s.render(c, http.StatusOK, "order_detail", gin.H{"Order": order})
}

// orderStatus 由 htmx 呼叫，回傳更新後的訂單面板；非 htmx 則整頁轉址。
func (s *Server) orderStatus(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		redirectMsg(c, "/orders", "", "訂單編號不合法")
		return
	}
	next := c.PostForm("status")
	if !validOrderStatus(next) {
		s.orderStatusFailed(c, id, "狀態值不合法")
		return
	}
	order, err := s.backend.UpdateOrderStatus(c.Request.Context(), id, next)
	if err != nil {
		s.orderStatusFailed(c, id, backendErrMsg(err))
		return
	}
	if isHTMX(c) {
		s.renderPartial(c, http.StatusOK, "order_panel", gin.H{
			"Order": order, "Msg": "訂單已更新為「" + orderStatusLabel(order.Status) + "」",
		})
		return
	}
	redirectMsg(c, "/orders/"+c.Param("id"), "訂單狀態已更新", "")
}

func (s *Server) orderStatusFailed(c *gin.Context, id int, msg string) {
	if !isHTMX(c) {
		redirectMsg(c, "/orders/"+c.Param("id"), "", msg)
		return
	}
	order, err := s.backend.GetOrder(c.Request.Context(), id)
	if err != nil {
		s.renderPartial(c, http.StatusOK, "order_panel_error", gin.H{"Err": msg})
		return
	}
	s.renderPartial(c, http.StatusOK, "order_panel", gin.H{"Order": order, "Err": msg})
}
