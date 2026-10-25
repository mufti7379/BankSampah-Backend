package middleware

import (
	"net/http"
	"net/url"
	"slices"

	"github.com/gin-gonic/gin"

	"github.com/banksampahku/backend/internal/utils"
)

// SecurityHeaders memasang header keamanan standar untuk API JSON.
// Cache-Control no-store mencegah response berisi data pribadi/token tersimpan
// di cache browser atau proxy; handler boleh menimpanya (misal gambar QR).
// HSTS hanya dikirim di production karena development memakai HTTP.
func SecurityHeaders(production bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.Writer.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		h.Set("Cache-Control", "no-store")
		if production {
			h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		c.Next()
	}
}

// BodyLimit menolak body request yang lebih besar dari maxBytes (413) agar
// server tidak kehabisan memori karena payload raksasa.
func BodyLimit(maxBytes int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.ContentLength > maxBytes {
			utils.RespondError(c, http.StatusRequestEntityTooLarge, "Ukuran data yang dikirim terlalu besar")
			return
		}
		// ContentLength bisa tidak diisi (chunked), jadi tetap batasi saat dibaca
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes)
		c.Next()
	}
}

// CORS hanya mengizinkan origin yang terdaftar (CORS_ALLOWED_ORIGINS) memanggil
// API dari browser dengan cookie. Origin lain tidak mendapat header CORS sehingga
// browser memblokir response-nya; preflight dari origin asing ditolak 403.
func CORS(allowedOrigins []string) gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin == "" {
			c.Next() // bukan request browser lintas origin (mobile, curl, same-origin)
			return
		}

		c.Writer.Header().Add("Vary", "Origin")
		allowed := slices.Contains(allowedOrigins, origin)
		if allowed {
			h := c.Writer.Header()
			h.Set("Access-Control-Allow-Origin", origin)
			h.Set("Access-Control-Allow-Credentials", "true")
			h.Set("Access-Control-Expose-Headers", "Content-Disposition, Retry-After")
		}

		if c.Request.Method == http.MethodOptions {
			if !allowed {
				c.AbortWithStatus(http.StatusForbidden)
				return
			}
			h := c.Writer.Header()
			h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Idempotency-Key")
			h.Set("Access-Control-Max-Age", "600")
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

// sensitiveQueryKeys adalah parameter query yang nilainya tidak boleh masuk log
var sensitiveQueryKeys = []string{"token", "password", "access_token"}

// RedactQuery menyamarkan nilai parameter sensitif di query string sebelum ditulis
// ke log, misal "token=abc" pada GET /auth/reset-password/verify.
func RedactQuery(rawQuery string) string {
	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		return "[query tidak valid]"
	}
	for _, key := range sensitiveQueryKeys {
		if _, ok := values[key]; ok {
			values.Set(key, "REDACTED")
		}
	}
	return values.Encode()
}
