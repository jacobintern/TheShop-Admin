// Package backend 是 TheShop-Backend 管理 API 的 HTTP client。
// 契約（2026-11-06 與 backend 定案）：
//   - 所有 /api/admin/* 帶 header X-Admin-Key
//   - 回應 {code, msg, data}，code 0 成功；錯誤 code 為 717665000 + 六碼
//   - 分頁 ?page=&fetch=，回 {pageNumber, fetch, list}
package backend

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	CodeInvalidAdminKey       = 717665000403002
	CodeInvalidArgument       = 717665000400001
	CodeProductNotFound       = 717665000400004
	CodeOrderNotFound         = 717665000400005
	CodeOrderStatusNotAllowed = 717665000400008

	ProductStatusOff = 0
	ProductStatusOn  = 1

	MaxFetch = 100
)

// Error 是 backend 回的業務錯誤（HTTP 非 2xx 或 code 非 0）。
type Error struct {
	HTTPStatus int
	Code       int
	Msg        string
}

func (e *Error) Error() string {
	return fmt.Sprintf("backend error: http=%d code=%d msg=%s", e.HTTPStatus, e.Code, e.Msg)
}

func IsCode(err error, code int) bool {
	var be *Error
	return errors.As(err, &be) && be.Code == code
}

type Page[T any] struct {
	PageNumber int `json:"pageNumber"`
	Fetch      int `json:"fetch"`
	List       []T `json:"list"`
}

type OrderItem struct {
	ProductID int     `json:"productId"`
	Num       int     `json:"num"`
	SubTotal  float64 `json:"subTotal"`
}

type OrderDelivery struct {
	DeliveryID    int    `json:"deliveryId"`
	StoreName     string `json:"storeName"`
	ReceiverName  string `json:"receiverName"`
	ReceiverPhone string `json:"receiverPhone"`
}

type Order struct {
	ID         int           `json:"id"`
	AccountID  int           `json:"accountId"`
	Total      float64       `json:"total"`
	Status     string        `json:"status"`
	Delivery   OrderDelivery `json:"delivery"`
	CreateTime string        `json:"createTime"`
	Items      []OrderItem   `json:"items"`
}

type ProductImage struct {
	ID   int    `json:"id,omitempty"`
	Name string `json:"name"`
	Type int    `json:"type"`
	URL  string `json:"url"`
}

type Product struct {
	ID     int            `json:"id"`
	Name   string         `json:"name"`
	Price  float64        `json:"price"`
	Stock  int            `json:"stock"`
	Spec   string         `json:"spec"`
	Status int            `json:"status"`
	Banner string         `json:"banner"`
	Images []ProductImage `json:"images"`
}

// ProductInput 是新增／修改商品的 body。
// backend 規則：修改時 images 沒帶就不動圖片、帶空陣列會清空，所以後台一律帶完整陣列（表單就是整組）。
type ProductInput struct {
	Name   string         `json:"name"`
	Price  float64        `json:"price"`
	Stock  int            `json:"stock"`
	Spec   string         `json:"spec"`
	Images []ProductImage `json:"images"`
}

type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

func New(baseURL, apiKey string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		http:    &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *Client) ListOrders(ctx context.Context, page, fetch int, status string) (Page[Order], error) {
	q := pageQuery(page, fetch)
	if status != "" {
		q.Set("status", status)
	}
	var out Page[Order]
	err := c.do(ctx, http.MethodGet, "/api/admin/order/list", q, nil, &out)
	return out, err
}

func (c *Client) GetOrder(ctx context.Context, id int) (Order, error) {
	var out Order
	err := c.do(ctx, http.MethodGet, fmt.Sprintf("/api/admin/order/%d", id), nil, nil, &out)
	return out, err
}

func (c *Client) UpdateOrderStatus(ctx context.Context, id int, status string) (Order, error) {
	var out Order
	err := c.do(ctx, http.MethodPut, fmt.Sprintf("/api/admin/order/%d/status", id), nil,
		map[string]string{"status": status}, &out)
	return out, err
}

func (c *Client) ListProducts(ctx context.Context, page, fetch int) (Page[Product], error) {
	var out Page[Product]
	err := c.do(ctx, http.MethodGet, "/api/admin/product/list", pageQuery(page, fetch), nil, &out)
	return out, err
}

func (c *Client) GetProduct(ctx context.Context, id int) (Product, error) {
	var out Product
	err := c.do(ctx, http.MethodGet, fmt.Sprintf("/api/admin/product/%d", id), nil, nil, &out)
	return out, err
}

func (c *Client) CreateProduct(ctx context.Context, in ProductInput) (int, error) {
	var out struct {
		ID int `json:"id"`
	}
	err := c.do(ctx, http.MethodPost, "/api/admin/product", nil, in, &out)
	return out.ID, err
}

func (c *Client) UpdateProduct(ctx context.Context, id int, in ProductInput) (Product, error) {
	var out Product
	err := c.do(ctx, http.MethodPut, fmt.Sprintf("/api/admin/product/%d", id), nil, in, &out)
	return out, err
}

// SetProductStatus 上架（1）／下架（0）。
func (c *Client) SetProductStatus(ctx context.Context, id, status int) error {
	return c.do(ctx, http.MethodPut, fmt.Sprintf("/api/admin/product/%d/status", id), nil,
		map[string]int{"status": status}, nil)
}

func pageQuery(page, fetch int) url.Values {
	if page < 1 {
		page = 1
	}
	if fetch < 1 {
		fetch = 20
	}
	if fetch > MaxFetch {
		fetch = MaxFetch
	}
	q := url.Values{}
	q.Set("page", fmt.Sprint(page))
	q.Set("fetch", fmt.Sprint(fetch))
	return q
}

type envelope struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

func (c *Client) do(ctx context.Context, method, path string, query url.Values, body, out any) error {
	u := c.baseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	var reader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, reader)
	if err != nil {
		return err
	}
	req.Header.Set("X-Admin-Key", c.apiKey)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("call backend %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	var env envelope
	if jsonErr := json.Unmarshal(raw, &env); jsonErr != nil {
		if resp.StatusCode >= 300 {
			return &Error{HTTPStatus: resp.StatusCode, Msg: http.StatusText(resp.StatusCode)}
		}
		return fmt.Errorf("decode backend response: %w", jsonErr)
	}
	if resp.StatusCode >= 300 || env.Code != 0 {
		return &Error{HTTPStatus: resp.StatusCode, Code: env.Code, Msg: env.Msg}
	}
	if out == nil || len(env.Data) == 0 || string(env.Data) == "null" {
		return nil
	}
	return json.Unmarshal(env.Data, out)
}
