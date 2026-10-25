package auth

import (
	"strings"

	"github.com/gin-gonic/gin"
)

const (
	// AccessTokenCookie adalah nama cookie httpOnly untuk client web
	AccessTokenCookie = "access_token"

	// ClientPlatformHeader = ClientPlatformMobile membuat login mengembalikan token di body
	ClientPlatformHeader = "X-Client-Platform"
	ClientPlatformMobile = "mobile"
	ClientPlatformWeb    = "web"

	contextClaimsKey = "auth_claims"
	contextUserKey   = "auth_user"
)

// SetSession menyimpan claims & user ke context (dipanggil middleware auth)
func SetSession(c *gin.Context, claims *Claims, user *User) {
	c.Set(contextClaimsKey, claims)
	c.Set(contextUserKey, user)
}

// GetClaims mengambil claims JWT dari context (nil jika belum melewati middleware auth)
func GetClaims(c *gin.Context) *Claims {
	if v, ok := c.Get(contextClaimsKey); ok {
		if claims, ok := v.(*Claims); ok {
			return claims
		}
	}
	return nil
}

// CurrentUser mengambil user yang sedang login dari context
// (data terbaru dari database, dimuat oleh middleware auth)
func CurrentUser(c *gin.Context) *User {
	if v, ok := c.Get(contextUserKey); ok {
		if user, ok := v.(*User); ok {
			return user
		}
	}
	return nil
}

// ClientPlatform mengembalikan "mobile" untuk request dari aplikasi Flutter, selain itu "web"
func ClientPlatform(c *gin.Context) string {
	if strings.EqualFold(c.GetHeader(ClientPlatformHeader), ClientPlatformMobile) {
		return ClientPlatformMobile
	}
	return ClientPlatformWeb
}

// ExtractToken membaca token dari "Authorization: Bearer <token>" (mobile)
// lalu fallback ke cookie httpOnly (web)
func ExtractToken(c *gin.Context) string {
	if header := c.GetHeader("Authorization"); header != "" {
		if parts := strings.SplitN(header, " ", 2); len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
			return strings.TrimSpace(parts[1])
		}
	}
	if cookie, err := c.Cookie(AccessTokenCookie); err == nil {
		return cookie
	}
	return ""
}
