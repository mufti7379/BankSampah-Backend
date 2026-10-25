package auth

import (
	"errors"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"

	"github.com/banksampahku/backend/internal/utils"
)

// Handler menangani endpoint HTTP /api/v1/auth/*
type Handler struct {
	service      *Service
	secureCookie bool
}

// NewHandler membuat handler auth. secureCookie=true di production (HTTPS).
func NewHandler(service *Service, secureCookie bool) *Handler {
	return &Handler{service: service, secureCookie: secureCookie}
}

// RegisterRoutes mendaftarkan route auth ke router group /api/v1/auth.
// requireAuth adalah middleware.RequireAuth (di-inject agar tidak terjadi import cycle).
func (h *Handler) RegisterRoutes(rg *gin.RouterGroup, requireAuth gin.HandlerFunc) {
	rg.POST("/register", h.Register)
	rg.POST("/login", h.Login)
	rg.POST("/logout", requireAuth, h.Logout)
	rg.GET("/me", requireAuth, h.Me)

	rg.POST("/forgot-password", h.ForgotPassword)
	rg.GET("/reset-password/verify", h.VerifyResetToken)
	rg.POST("/reset-password", h.ResetPassword)
}

// ForgotPassword: POST /api/v1/auth/forgot-password
// Respons selalu sama (terdaftar/tidak) agar email tidak bisa dienumerasi.
func (h *Handler) ForgotPassword(c *gin.Context) {
	var req ForgotPasswordRequest
	if !utils.BindJSON(c, &req) {
		return
	}
	if err := h.service.ForgotPassword(c.Request.Context(), req.Email, c.ClientIP()); err != nil {
		// Tetap kirim respons sukses ke client; error hanya dicatat di log
		log.Error().Err(err).Msg("Gagal memproses lupa password")
	}
	utils.RespondSuccess(c, http.StatusOK,
		"Jika email terdaftar, link untuk mengatur ulang password telah dikirim. Silakan periksa kotak masuk atau folder spam Anda.",
		nil)
}

// VerifyResetToken: GET /api/v1/auth/reset-password/verify?token=...
// Dipanggil halaman reset password sebelum menampilkan form.
func (h *Handler) VerifyResetToken(c *gin.Context) {
	token := c.Query("token")
	if token == "" {
		utils.RespondError(c, http.StatusBadRequest, "Token reset wajib diisi")
		return
	}
	user, err := h.service.VerifyResetToken(c.Request.Context(), token)
	if err != nil {
		if errors.Is(err, ErrInvalidResetToken) {
			utils.RespondError(c, http.StatusBadRequest, capitalize(err.Error()))
			return
		}
		utils.RespondInternal(c, err)
		return
	}
	utils.RespondSuccess(c, http.StatusOK, "Token reset valid", gin.H{
		"email":     maskEmail(user.Email),
		"full_name": user.FullName,
	})
}

// ResetPassword: POST /api/v1/auth/reset-password
func (h *Handler) ResetPassword(c *gin.Context) {
	var req ResetPasswordRequest
	if !utils.BindJSON(c, &req) {
		return
	}
	if err := utils.ValidatePasswordStrength(req.Password); err != nil {
		utils.RespondValidationError(c, map[string]string{"password": err.Error()})
		return
	}
	if err := h.service.ResetPassword(c.Request.Context(), req.Token, req.Password); err != nil {
		if errors.Is(err, ErrInvalidResetToken) {
			utils.RespondError(c, http.StatusBadRequest, capitalize(err.Error()))
			return
		}
		utils.RespondInternal(c, err)
		return
	}
	utils.RespondSuccess(c, http.StatusOK,
		"Password berhasil diubah. Silakan login dengan password baru Anda.", nil)
}

// maskEmail menyamarkan email untuk ditampilkan, misal "aisyah@x.id" -> "ai****@x.id"
func maskEmail(email string) string {
	at := strings.LastIndex(email, "@")
	if at <= 0 {
		return email
	}
	visible := 2
	if at < visible {
		visible = at
	}
	return email[:visible] + strings.Repeat("*", 4) + email[at:]
}

// Register: POST /api/v1/auth/register
func (h *Handler) Register(c *gin.Context) {
	var req RegisterRequest
	if !utils.BindJSON(c, &req) {
		return
	}
	if err := utils.ValidatePasswordStrength(req.Password); err != nil {
		utils.RespondValidationError(c, map[string]string{"password": err.Error()})
		return
	}

	user, err := h.service.Register(c.Request.Context(), req)
	if err != nil {
		switch {
		case errors.Is(err, ErrEmailTaken):
			utils.RespondValidationError(c, map[string]string{"email": "Email sudah terdaftar"})
		case errors.Is(err, ErrSchoolNotFound):
			utils.RespondValidationError(c, map[string]string{"school_id": "Sekolah tidak ditemukan"})
		default:
			utils.RespondInternal(c, err)
		}
		return
	}

	utils.RespondSuccess(c, http.StatusCreated,
		"Pendaftaran berhasil. Akun Anda sedang menunggu verifikasi admin bank sampah.",
		user.ToResponse())
}

// Login: POST /api/v1/auth/login
func (h *Handler) Login(c *gin.Context) {
	var req LoginRequest
	if !utils.BindJSON(c, &req) {
		return
	}

	result, err := h.service.Login(c.Request.Context(), req)
	if err != nil {
		var locked *LockedError
		switch {
		case errors.As(err, &locked):
			c.Header("Retry-After", strconv.Itoa(int(math.Ceil(locked.RetryAfter.Seconds()))))
			utils.RespondError(c, http.StatusTooManyRequests, capitalize(locked.Error()))
		case errors.Is(err, ErrInvalidCredentials):
			utils.RespondError(c, http.StatusUnauthorized, "Email atau password salah")
		case errors.Is(err, ErrAccountPending), errors.Is(err, ErrAccountInactive):
			utils.RespondError(c, http.StatusForbidden, capitalize(err.Error()))
		default:
			utils.RespondInternal(c, err)
		}
		return
	}

	h.respondLogin(c, result)
}

// respondLogin: mobile (header X-Client-Platform: mobile) menerima token di body untuk
// disimpan di flutter_secure_storage; web hanya menerima cookie httpOnly sehingga
// token tidak pernah bisa dibaca JavaScript.
func (h *Handler) respondLogin(c *gin.Context, result *LoginResponse) {
	msg := "Login berhasil, selamat datang " + result.User.FullName
	if ClientPlatform(c) == ClientPlatformMobile {
		utils.RespondSuccess(c, http.StatusOK, msg, result)
		return
	}

	h.setTokenCookie(c, result.AccessToken, int(h.service.Tokens.TTL().Seconds()))
	web := *result
	web.AccessToken, web.TokenType = "", ""
	utils.RespondSuccess(c, http.StatusOK, msg, web)
}

// Logout: POST /api/v1/auth/logout
func (h *Handler) Logout(c *gin.Context) {
	h.service.Logout(GetClaims(c))
	h.setTokenCookie(c, "", -1)
	utils.RespondSuccess(c, http.StatusOK, "Logout berhasil", nil)
}

// Me: GET /api/v1/auth/me
// User sudah dimuat terbaru dari database oleh middleware.RequireAuth.
func (h *Handler) Me(c *gin.Context) {
	utils.RespondSuccess(c, http.StatusOK, "Profil berhasil diambil", CurrentUser(c).ToResponse())
}

// setTokenCookie menulis/menghapus cookie token (httpOnly, SameSite=Lax, Secure di production)
func (h *Handler) setTokenCookie(c *gin.Context, token string, maxAge int) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(AccessTokenCookie, token, maxAge, "/", "", h.secureCookie, true)
}

// capitalize membuat huruf pertama pesan menjadi kapital
func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
