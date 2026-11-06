package backend

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_ListOrders_SendsKeyAndParsesPage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/admin/order/list", r.URL.Path)
		assert.Equal(t, "k", r.Header.Get("X-Admin-Key"))
		assert.Equal(t, "2", r.URL.Query().Get("page"))
		assert.Equal(t, "20", r.URL.Query().Get("fetch"))
		assert.Equal(t, "paid", r.URL.Query().Get("status"))
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0, "msg": "success",
			"data": map[string]any{"pageNumber": 2, "fetch": 20, "list": []map[string]any{{"id": 7, "status": "paid", "total": 99.5}}},
		})
	}))
	defer srv.Close()

	c := New(srv.URL, "k")
	page, err := c.ListOrders(context.Background(), 2, 20, "paid")
	require.NoError(t, err)
	require.Len(t, page.List, 1)
	assert.Equal(t, 7, page.List[0].ID)
	assert.Equal(t, 99.5, page.List[0].Total)
}

func TestClient_UpdateOrderStatus_BusinessError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPut, r.Method)
		var body map[string]string
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.Equal(t, "shipped", body["status"])
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": CodeOrderStatusNotAllowed, "msg": "order status does not allow this operation.",
		})
	}))
	defer srv.Close()

	_, err := New(srv.URL, "k").UpdateOrderStatus(context.Background(), 1, "shipped")
	require.Error(t, err)
	assert.True(t, IsCode(err, CodeOrderStatusNotAllowed))
}

func TestClient_ForbiddenWithoutJSONBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer srv.Close()

	err := New(srv.URL, "k").SetProductStatus(context.Background(), 1, ProductStatusOff)
	var be *Error
	require.ErrorAs(t, err, &be)
	assert.Equal(t, http.StatusForbidden, be.HTTPStatus)
}

func TestClient_UpdateProduct_EmptyImagesIsSentAsArray(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var raw map[string]json.RawMessage
		require.NoError(t, json.NewDecoder(r.Body).Decode(&raw))
		assert.JSONEq(t, "[]", string(raw["images"]))
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "msg": "success", "data": map[string]any{"id": 1}})
	}))
	defer srv.Close()

	_, err := New(srv.URL, "k").UpdateProduct(context.Background(), 1, ProductInput{Name: "x", Images: []ProductImage{}})
	require.NoError(t, err)
}

func TestClient_CreateProduct(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/api/admin/product", r.URL.Path)
		var in ProductInput
		require.NoError(t, json.NewDecoder(r.Body).Decode(&in))
		assert.Equal(t, "牛奶", in.Name)
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "msg": "success", "data": map[string]int{"id": 42}})
	}))
	defer srv.Close()

	id, err := New(srv.URL, "k").CreateProduct(context.Background(), ProductInput{Name: "牛奶", Price: 50, Stock: 3})
	require.NoError(t, err)
	assert.Equal(t, 42, id)
}
