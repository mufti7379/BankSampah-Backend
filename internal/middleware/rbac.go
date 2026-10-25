package middleware

import (
	"net/http"
	"slices"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"

	"github.com/banksampahku/backend/internal/auth"
	"github.com/banksampahku/backend/internal/utils"
)

// RequireRole membatasi akses hanya untuk role tertentu (PRD Bab 5).
// Wajib dipasang SETELAH RequireAuth. Role dibaca dari data user terbaru di
// database (bukan dari claim JWT), sehingga perubahan role langsung berlaku.
//
//	admin := api.Group("/admin", middleware.RequireAuth(svc), middleware.RequireRole(auth.RoleAdmin))
func RequireRole(roles ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		user := auth.CurrentUser(c)
		if user == nil {
			// Salah susun middleware: RequireRole dipasang tanpa RequireAuth
			log.Error().Str("path", c.FullPath()).Msg("RequireRole dipanggil tanpa RequireAuth")
			utils.RespondError(c, http.StatusUnauthorized, "Silakan login terlebih dahulu")
			return
		}

		if !slices.Contains(roles, user.Role) {
			log.Warn().
				Str("user_id", user.ID).
				Str("role", user.Role).
				Strs("required", roles).
				Str("path", c.Request.URL.Path).
				Msg("Akses ditolak: role tidak sesuai")
			utils.RespondError(c, http.StatusForbidden, "Anda tidak memiliki akses ke fitur ini")
			return
		}
		c.Next()
	}
}
