package web

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/jacobintern/TheShop/admin/internal/backend"
)

func (s *Server) productList(c *gin.Context) {
	page := pageParam(c)
	result, err := s.backend.ListProducts(c.Request.Context(), page, defaultFetch)
	data := gin.H{
		"Page":     page,
		"Products": result.List,
		"HasNext":  len(result.List) >= defaultFetch,
	}
	if err != nil {
		data["Err"] = backendErrMsg(err)
	}
	s.render(c, http.StatusOK, "products", data)
}

func (s *Server) productNew(c *gin.Context) {
	s.render(c, http.StatusOK, "product_form", gin.H{"Form": productForm{Stock: 0}, "IsNew": true})
}

func (s *Server) productCreate(c *gin.Context) {
	form, errMsg := parseProductForm(c)
	if errMsg != "" {
		s.render(c, http.StatusBadRequest, "product_form", gin.H{"Form": form, "IsNew": true, "Err": errMsg})
		return
	}
	id, err := s.backend.CreateProduct(c.Request.Context(), form.toInput())
	if err != nil {
		s.render(c, http.StatusBadGateway, "product_form", gin.H{"Form": form, "IsNew": true, "Err": backendErrMsg(err)})
		return
	}
	redirectMsg(c, "/products/"+strconv.Itoa(id)+"/edit", "商品已新增", "")
}

func (s *Server) productEdit(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		redirectMsg(c, "/products", "", "商品編號不合法")
		return
	}
	p, err := s.backend.GetProduct(c.Request.Context(), id)
	if err != nil {
		redirectMsg(c, "/products", "", backendErrMsg(err))
		return
	}
	s.render(c, http.StatusOK, "product_form", gin.H{"Form": formFromProduct(p), "Product": p})
}

func (s *Server) productUpdate(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		redirectMsg(c, "/products", "", "商品編號不合法")
		return
	}
	form, errMsg := parseProductForm(c)
	form.ID = id
	if errMsg != "" {
		s.render(c, http.StatusBadRequest, "product_form", gin.H{"Form": form, "Err": errMsg})
		return
	}
	if _, err := s.backend.UpdateProduct(c.Request.Context(), id, form.toInput()); err != nil {
		s.render(c, http.StatusBadGateway, "product_form", gin.H{"Form": form, "Err": backendErrMsg(err)})
		return
	}
	redirectMsg(c, "/products/"+strconv.Itoa(id)+"/edit", "商品已更新", "")
}

// productStatus 上架／下架（下架等同 backend 的 DELETE，不硬刪）。
func (s *Server) productStatus(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		redirectMsg(c, "/products", "", "商品編號不合法")
		return
	}
	status, err := strconv.Atoi(c.PostForm("status"))
	if err != nil || (status != backend.ProductStatusOn && status != backend.ProductStatusOff) {
		redirectMsg(c, c.DefaultPostForm("back", "/products"), "", "狀態值不合法")
		return
	}
	if err := s.backend.SetProductStatus(c.Request.Context(), id, status); err != nil {
		redirectMsg(c, c.DefaultPostForm("back", "/products"), "", backendErrMsg(err))
		return
	}
	msg := "商品已下架"
	if status == backend.ProductStatusOn {
		msg = "商品已上架"
	}
	redirectMsg(c, c.DefaultPostForm("back", "/products"), msg, "")
}

// productForm 是表單欄位的字串版本，驗證失敗時能原樣回填。
type productForm struct {
	ID     int
	Name   string
	Price  string
	Stock  int
	Spec   string
	Banner string
	Images string // 一行一個 URL，type=1
}

func formFromProduct(p backend.Product) productForm {
	f := productForm{
		ID:    p.ID,
		Name:  p.Name,
		Price: strconv.FormatFloat(p.Price, 'f', -1, 64),
		Stock: p.Stock,
		Spec:  p.Spec,
	}
	var others []string
	for _, img := range p.Images {
		if img.Type == 0 && f.Banner == "" {
			f.Banner = img.URL
			continue
		}
		others = append(others, img.URL)
	}
	f.Images = strings.Join(others, "\n")
	return f
}

func parseProductForm(c *gin.Context) (productForm, string) {
	f := productForm{
		Name:   strings.TrimSpace(c.PostForm("name")),
		Price:  strings.TrimSpace(c.PostForm("price")),
		Spec:   strings.TrimSpace(c.PostForm("spec")),
		Banner: strings.TrimSpace(c.PostForm("banner")),
		Images: strings.TrimSpace(c.PostForm("images")),
	}
	stock, err := strconv.Atoi(strings.TrimSpace(c.PostForm("stock")))
	if err != nil || stock < 0 {
		return f, "庫存必須是 0 以上的整數"
	}
	f.Stock = stock
	if f.Name == "" {
		return f, "商品名稱必填"
	}
	price, err := strconv.ParseFloat(f.Price, 64)
	if err != nil || price < 0 {
		return f, "價格必須是 0 以上的數字"
	}
	return f, ""
}

// toInput 一律送 images（整組替換），表單已回填現有圖片，所以沒改就是原樣。
func (f productForm) toInput() backend.ProductInput {
	price, _ := strconv.ParseFloat(f.Price, 64)
	in := backend.ProductInput{
		Name:   f.Name,
		Price:  price,
		Stock:  f.Stock,
		Spec:   f.Spec,
		Images: []backend.ProductImage{},
	}
	if f.Banner != "" {
		in.Images = append(in.Images, backend.ProductImage{Name: "banner", Type: 0, URL: f.Banner})
	}
	for i, line := range strings.Split(f.Images, "\n") {
		u := strings.TrimSpace(line)
		if u == "" {
			continue
		}
		in.Images = append(in.Images, backend.ProductImage{Name: "image-" + strconv.Itoa(i+1), Type: 1, URL: u})
	}
	return in
}
