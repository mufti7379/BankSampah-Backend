package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestClientPlatform(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for header, want := range map[string]string{
		"":        ClientPlatformWeb,
		"mobile":  ClientPlatformMobile,
		"MOBILE":  ClientPlatformMobile,
		"android": ClientPlatformWeb, // nilai lain dianggap web
	} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
		if header != "" {
			c.Request.Header.Set(ClientPlatformHeader, header)
		}
		if got := ClientPlatform(c); got != want {
			t.Errorf("header %q: ClientPlatform = %q, want %q", header, got, want)
		}
	}
}

func TestRespondLogin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewHandler(NewService(Deps{Tokens: NewTokenManager(strings.Repeat("s", 32), 168)}), true)
	result := &LoginResponse{
		AccessToken: "jwt-rahasia",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(time.Hour),
		User:        UserResponse{ID: "u1", FullName: "Aisyah"},
	}

	call := func(platform string) (*httptest.ResponseRecorder, map[string]any) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
		if platform != "" {
			c.Request.Header.Set(ClientPlatformHeader, platform)
		}
		h.respondLogin(c, result)

		var body struct {
			Data map[string]any `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("body bukan JSON: %v", err)
		}
		return w, body.Data
	}

	t.Run("web: token hanya di cookie httpOnly", func(t *testing.T) {
		w, data := call("")
		if _, ok := data["access_token"]; ok {
			t.Error("access_token tidak boleh ada di body untuk web")
		}
		if _, ok := data["token_type"]; ok {
			t.Error("token_type tidak boleh ada di body untuk web")
		}
		if data["user"] == nil || data["expires_at"] == nil {
			t.Errorf("user & expires_at tetap harus dikirim: %v", data)
		}
		cookie := w.Header().Get("Set-Cookie")
		if !strings.Contains(cookie, AccessTokenCookie+"=jwt-rahasia") ||
			!strings.Contains(cookie, "HttpOnly") || !strings.Contains(cookie, "Secure") {
			t.Errorf("cookie tidak sesuai: %q", cookie)
		}
	})

	t.Run("mobile: token di body tanpa cookie", func(t *testing.T) {
		w, data := call("Mobile")
		if data["access_token"] != "jwt-rahasia" || data["token_type"] != "Bearer" {
			t.Errorf("token mobile tidak sesuai: %v", data)
		}
		if cookie := w.Header().Get("Set-Cookie"); cookie != "" {
			t.Errorf("mobile tidak perlu cookie: %q", cookie)
		}
	})

	if result.AccessToken != "jwt-rahasia" {
		t.Error("respondLogin tidak boleh mengubah result asli")
	}
}
