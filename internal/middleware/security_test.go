package middleware

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestSecurityHeaders(t *testing.T) {
	r := gin.New()
	r.Use(SecurityHeaders(true))
	r.GET("/x", func(c *gin.Context) { c.Status(http.StatusOK) })
	r.GET("/qr", func(c *gin.Context) { c.Header("Cache-Control", "private, max-age=300"); c.Status(http.StatusOK) })

	h := do(r, http.MethodGet, "/x", "").Header()
	for key, want := range map[string]string{
		"X-Content-Type-Options":    "nosniff",
		"X-Frame-Options":           "DENY",
		"Cache-Control":             "no-store",
		"Strict-Transport-Security": "max-age=31536000; includeSubDomains",
	} {
		if got := h.Get(key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
	if got := do(r, http.MethodGet, "/qr", "").Header().Get("Cache-Control"); got != "private, max-age=300" {
		t.Errorf("handler tidak bisa menimpa Cache-Control: %q", got)
	}

	dev := gin.New()
	dev.Use(SecurityHeaders(false))
	dev.GET("/x", func(c *gin.Context) { c.Status(http.StatusOK) })
	if got := do(dev, http.MethodGet, "/x", "").Header().Get("Strict-Transport-Security"); got != "" {
		t.Errorf("HSTS di development = %q, want kosong", got)
	}
}

func TestBodyLimit(t *testing.T) {
	r := gin.New()
	r.Use(BodyLimit(16))
	r.POST("/x", func(c *gin.Context) {
		if _, err := io.ReadAll(c.Request.Body); err != nil {
			c.Status(http.StatusBadRequest)
			return
		}
		c.Status(http.StatusOK)
	})

	post := func(body io.Reader, contentLength int64) int {
		req := httptest.NewRequest(http.MethodPost, "/x", body)
		req.ContentLength = contentLength
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w.Code
	}

	if got := post(strings.NewReader("kecil"), 5); got != http.StatusOK {
		t.Errorf("body kecil = %d, want 200", got)
	}
	big := strings.Repeat("a", 100)
	if got := post(strings.NewReader(big), 100); got != http.StatusRequestEntityTooLarge {
		t.Errorf("body besar = %d, want 413", got)
	}
	// Tanpa Content-Length (chunked): tetap dipotong saat dibaca
	if got := post(strings.NewReader(big), -1); got != http.StatusBadRequest {
		t.Errorf("body besar tanpa Content-Length = %d, want 400", got)
	}
}

func TestCORS(t *testing.T) {
	r := gin.New()
	r.Use(CORS([]string{"https://admin.banksampahku.id"}))
	r.GET("/x", func(c *gin.Context) { c.Status(http.StatusOK) })

	call := func(method, origin string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/x", nil)
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	w := call(http.MethodGet, "https://admin.banksampahku.id")
	if w.Header().Get("Access-Control-Allow-Origin") != "https://admin.banksampahku.id" ||
		w.Header().Get("Access-Control-Allow-Credentials") != "true" {
		t.Errorf("origin terdaftar tidak mendapat header CORS: %v", w.Header())
	}

	w = call(http.MethodGet, "https://jahat.example")
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("origin asing mendapat Allow-Origin = %q", got)
	}

	if w := call(http.MethodOptions, "https://admin.banksampahku.id"); w.Code != http.StatusNoContent ||
		w.Header().Get("Access-Control-Allow-Methods") == "" ||
		!strings.Contains(w.Header().Get("Access-Control-Allow-Headers"), "Idempotency-Key") {
		t.Errorf("preflight origin terdaftar = %d, header %v", w.Code, w.Header())
	}
	if w := call(http.MethodOptions, "https://jahat.example"); w.Code != http.StatusForbidden {
		t.Errorf("preflight origin asing = %d, want 403", w.Code)
	}
	if w := call(http.MethodGet, ""); w.Code != http.StatusOK {
		t.Errorf("request tanpa Origin (mobile) = %d, want 200", w.Code)
	}
}

func TestRedactQuery(t *testing.T) {
	cases := map[string]string{
		"token=rahasia123":         "token=REDACTED",
		"page=2&token=abc":         "page=2&token=REDACTED",
		"search=budi&status=aktif": "search=budi&status=aktif",
		"%zz":                      "[query tidak valid]",
	}
	for in, want := range cases {
		if got := RedactQuery(in); got != want {
			t.Errorf("RedactQuery(%q) = %q, want %q", in, got, want)
		}
	}
}
