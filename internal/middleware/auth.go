// Package middleware berisi middleware HTTP bersama: autentikasi JWT, RBAC,
// audit log, dan rate limiting.
package middleware

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"

	"github.com/banksampahku/backend/internal/auth"
	"github.com/banksampahku/backend/internal/utils"
)

// SessionResolver memverifikasi token dan memuat user (diimplementasikan auth.Service)
type SessionResolver interface {
	ResolveSession(ctx context.Context, token string) (*auth.Claims, *auth.User, error)
}

// RequireAuth memverifikasi JWT (header "Authorization: Bearer" atau cookie httpOnly),
// memuat user terbaru dari database, lalu menyimpannya ke context.
// Ambil di handler dengan auth.CurrentUser(c) / auth.GetClaims(c).
func RequireAuth(resolver SessionResolver) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := auth.ExtractToken(c)
		if token == "" {
			utils.RespondError(c, http.StatusUnauthorized, "Silakan login terlebih dahulu")
			return
		}

		claims, user, err := resolver.ResolveSession(c.Request.Context(), token)
		if err != nil {
			switch {
			case errors.Is(err, auth.ErrInvalidToken):
				utils.RespondError(c, http.StatusUnauthorized, "Sesi tidak valid atau telah berakhir, silakan login kembali")
			case errors.Is(err, auth.ErrSessionExpired):
				utils.RespondError(c, http.StatusUnauthorized, "Password akun Anda telah diubah, silakan login kembali")
			case errors.Is(err, auth.ErrAccountInactive):
				utils.RespondError(c, http.StatusForbidden, "Akun Anda telah dinonaktifkan, silakan hubungi admin bank sampah")
			case errors.Is(err, auth.ErrAccountPending):
				utils.RespondError(c, http.StatusForbidden, "Akun Anda sedang menunggu verifikasi admin")
			default:
				log.Error().Err(err).Msg("Gagal memverifikasi sesi")
				utils.RespondError(c, http.StatusInternalServerError, "Terjadi kesalahan pada server, silakan coba lagi")
			}
			return
		}

		auth.SetSession(c, claims, user)
		c.Next()
	}
}
