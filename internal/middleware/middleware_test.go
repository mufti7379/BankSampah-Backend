package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/banksampahku/backend/internal/audit"
	"github.com/banksampahku/backend/internal/auth"
)

func init() { gin.SetMode(gin.TestMode) }

// fakeResolver memetakan token → user (atau error) tanpa database
type fakeResolver map[string]struct {
	user *auth.User
	err  error
}

func (f fakeResolver) ResolveSession(_ context.Context, token string) (*auth.Claims, *auth.User, error) {
	r, ok := f[token]
	if !ok {
		return nil, nil, auth.ErrInvalidToken
	}
	if r.err != nil {
		return nil, nil, r.err
	}
	return &auth.Claims{Role: r.user.Role}, r.user, nil
}

// fakeRecorder menampung log audit di memori
type fakeRecorder struct {
	mu      sync.Mutex
	entries []audit.Entry
}

func (f *fakeRecorder) Record(_ context.Context, e audit.Entry) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.entries = append(f.entries, e)
	return nil
}

var resolver = fakeResolver{
	"token-admin":    {user: &auth.User{ID: "11111111-1111-1111-1111-111111111111", Role: auth.RoleAdmin}},
	"token-nasabah":  {user: &auth.User{ID: "22222222-2222-2222-2222-222222222222", Role: auth.RoleNasabah}},
	"token-inactive": {err: auth.ErrAccountInactive},
	"token-pwchange": {err: auth.ErrSessionExpired},
}

func do(r http.Handler, method, path, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestRequireAuthAndRole(t *testing.T) {
	r := gin.New()
	ok := func(c *gin.Context) { c.Status(http.StatusOK) }
	r.GET("/admin/x", RequireAuth(resolver), RequireRole(auth.RoleAdmin), ok)
	r.GET("/nasabah/x", RequireAuth(resolver), RequireRole(auth.RoleNasabah), ok)
	r.GET("/both/x", RequireAuth(resolver), RequireRole(auth.RoleAdmin, auth.RoleNasabah), ok)
	r.GET("/no-auth/x", RequireRole(auth.RoleAdmin), ok) // salah susun middleware

	cases := []struct {
		name, path, token string
		want              int
	}{
		{"tanpa token", "/admin/x", "", http.StatusUnauthorized},
		{"token palsu", "/admin/x", "token-palsu", http.StatusUnauthorized},
		{"akun nonaktif", "/admin/x", "token-inactive", http.StatusForbidden},
		{"password sudah diganti", "/admin/x", "token-pwchange", http.StatusUnauthorized},
		{"admin ke area admin", "/admin/x", "token-admin", http.StatusOK},
		{"nasabah ke area admin", "/admin/x", "token-nasabah", http.StatusForbidden},
		{"admin ke area nasabah", "/nasabah/x", "token-admin", http.StatusForbidden},
		{"nasabah ke area nasabah", "/nasabah/x", "token-nasabah", http.StatusOK},
		{"nasabah ke area bersama", "/both/x", "token-nasabah", http.StatusOK},
		{"RequireRole tanpa RequireAuth", "/no-auth/x", "token-admin", http.StatusUnauthorized},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := do(r, http.MethodGet, tc.path, tc.token).Code; got != tc.want {
				t.Errorf("status = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestAudit(t *testing.T) {
	rec := &fakeRecorder{}
	r := gin.New()
	chain := []gin.HandlerFunc{RequireAuth(resolver), RequireRole(auth.RoleAdmin)}

	r.PATCH("/nasabah/:id", append(chain,
		Audit(rec, audit.ActionNasabahUpdate, audit.EntityUser),
		func(c *gin.Context) {
			SetAuditMetadata(c, map[string]any{"before": "8A", "after": "9A"})
			c.Status(http.StatusOK)
		})...)
	r.POST("/nasabah", append(chain,
		Audit(rec, audit.ActionNasabahCreate, audit.EntityUser),
		func(c *gin.Context) {
			SetAuditEntityID(c, "33333333-3333-3333-3333-333333333333")
			c.Status(http.StatusCreated)
		})...)
	r.POST("/gagal", append(chain,
		Audit(rec, audit.ActionNasabahCreate, audit.EntityUser),
		func(c *gin.Context) { c.Status(http.StatusUnprocessableEntity) })...)
	r.POST("/weighings", append(chain,
		Audit(rec, audit.ActionWeighingCreate, audit.EntityWeighing),
		func(c *gin.Context) { SkipAudit(c); c.Status(http.StatusCreated) })...)

	do(r, http.MethodPatch, "/nasabah/44444444-4444-4444-4444-444444444444", "token-admin")
	do(r, http.MethodPost, "/nasabah", "token-admin")
	do(r, http.MethodPost, "/gagal", "token-admin")     // 422 → tidak dicatat
	do(r, http.MethodPost, "/weighings", "token-admin") // SkipAudit → tidak dicatat
	do(r, http.MethodPost, "/nasabah", "token-nasabah") // 403 → tidak dicatat

	if len(rec.entries) != 2 {
		t.Fatalf("jumlah log = %d, want 2: %+v", len(rec.entries), rec.entries)
	}

	upd := rec.entries[0]
	if upd.Action != audit.ActionNasabahUpdate || upd.EntityID != "44444444-4444-4444-4444-444444444444" ||
		upd.ActorID != "11111111-1111-1111-1111-111111111111" || upd.Metadata["after"] != "9A" ||
		upd.Metadata["request"].(map[string]any)["method"] != http.MethodPatch {
		t.Errorf("log update tidak sesuai: %+v", upd)
	}

	create := rec.entries[1]
	if create.Action != audit.ActionNasabahCreate || create.EntityID != "33333333-3333-3333-3333-333333333333" {
		t.Errorf("log create tidak sesuai: %+v", create)
	}
}

func TestIPRateLimiter(t *testing.T) {
	limiter := NewIPRateLimiter("test", 3, 3)
	r := gin.New()
	r.GET("/x", limiter.Middleware(), func(c *gin.Context) { c.Status(http.StatusOK) })

	call := func(ip string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/x", nil)
		req.RemoteAddr = ip + ":12345"
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	for i := 1; i <= 3; i++ {
		if w := call("10.0.0.1"); w.Code != http.StatusOK {
			t.Fatalf("request ke-%d = %d, want 200", i, w.Code)
		}
	}
	w := call("10.0.0.1")
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("request ke-4 = %d, want 429", w.Code)
	}
	if w.Header().Get("Retry-After") == "" {
		t.Error("header Retry-After kosong")
	}
	// IP lain punya kuota sendiri
	if w := call("10.0.0.2"); w.Code != http.StatusOK {
		t.Errorf("IP lain = %d, want 200", w.Code)
	}
}
