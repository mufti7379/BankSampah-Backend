package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/banksampahku/backend/internal/utils"
)

// ValidUUIDParam menolak path param (misal ":id") yang bukan UUID dengan 404,
// agar input asal-asalan tidak sampai ke query PostgreSQL.
func ValidUUIDParam(param, notFoundMessage string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !utils.IsUUID(c.Param(param)) {
			utils.RespondError(c, http.StatusNotFound, notFoundMessage)
			return
		}
		c.Next()
	}
}
