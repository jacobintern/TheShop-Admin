package web

import (
	"io"
	"net/http"
	"strings"
)

func copyBody(dst *strings.Builder, resp *http.Response) (int64, error) {
	return io.Copy(dst, resp.Body)
}
