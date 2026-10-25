package weighing

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestCreateRejectsInvalidIdempotencyKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	// service nil: header tidak valid harus ditolak sebelum service/database disentuh
	r := gin.New()
	r.POST("/weighings", (&Handler{}).Create)

	for _, key := range []string{"123", "bukan-uuid", strings.Repeat("a", 36)} {
		req := httptest.NewRequest(http.MethodPost, "/weighings", strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(IdempotencyKeyHeader, key)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusUnprocessableEntity {
			t.Fatalf("key %q: status = %d, want 422", key, w.Code)
		}
		var body struct {
			Errors map[string]string `json:"errors"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &body)
		if body.Errors["idempotency_key"] == "" {
			t.Errorf("key %q: error idempotency_key kosong: %s", key, w.Body.String())
		}
	}
}
