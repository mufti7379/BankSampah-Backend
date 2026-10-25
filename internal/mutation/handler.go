package mutation

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/banksampahku/backend/internal/auth"
	"github.com/banksampahku/backend/internal/middleware"
	"github.com/banksampahku/backend/internal/utils"
)

// Handler menangani endpoint baca mutasi saldo (admin & nasabah)
type Handler struct {
	service *Service
}

// NewHandler membuat handler mutasi
func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// RegisterAdminRoutes mendaftarkan route ke group /api/v1/admin
func (h *Handler) RegisterAdminRoutes(admin *gin.RouterGroup) {
	admin.GET("/mutations", h.ListAll)
	admin.GET("/nasabah/:id/mutations", middleware.ValidUUIDParam("id", "Nasabah tidak ditemukan"), h.ListForNasabah)
}

// RegisterNasabahRoutes mendaftarkan route ke group /api/v1/nasabah
func (h *Handler) RegisterNasabahRoutes(nasabah *gin.RouterGroup) {
	nasabah.GET("/me/mutations", h.ListMine)
	nasabah.GET("/me/balance", h.MyBalance)
}

// ListMine: GET /api/v1/nasabah/me/mutations?type=&month=&date_from=&date_to=&page=&limit=
func (h *Handler) ListMine(c *gin.Context) {
	var q FilterQuery
	if !utils.BindQuery(c, &q) {
		return
	}
	q.Normalize()
	res, err := h.service.ListMine(c.Request.Context(), auth.CurrentUser(c), q)
	if err != nil {
		h.respondError(c, err)
		return
	}
	utils.RespondSuccess(c, http.StatusOK, "Riwayat saldo berhasil diambil", res)
}

// MyBalance: GET /api/v1/nasabah/me/balance
func (h *Handler) MyBalance(c *gin.Context) {
	res, err := h.service.MyBalance(c.Request.Context(), auth.CurrentUser(c))
	if err != nil {
		h.respondError(c, err)
		return
	}
	utils.RespondSuccess(c, http.StatusOK, "Saldo berhasil diambil", res)
}

// ListAll: GET /api/v1/admin/mutations?nasabah_id=&search=&type=&reference_type=&month=&date_from=&date_to=
func (h *Handler) ListAll(c *gin.Context) {
	var q AdminListQuery
	if !utils.BindQuery(c, &q) {
		return
	}
	q.Normalize()
	res, err := h.service.ListAll(c.Request.Context(), auth.CurrentUser(c), q)
	if err != nil {
		h.respondError(c, err)
		return
	}
	utils.RespondSuccess(c, http.StatusOK, "Daftar mutasi saldo berhasil diambil", res)
}

// ListForNasabah: GET /api/v1/admin/nasabah/:id/mutations?type=&month=&date_from=&date_to=
func (h *Handler) ListForNasabah(c *gin.Context) {
	var q FilterQuery
	if !utils.BindQuery(c, &q) {
		return
	}
	q.Normalize()
	res, err := h.service.ListForNasabah(c.Request.Context(), auth.CurrentUser(c), c.Param("id"), q)
	if err != nil {
		h.respondError(c, err)
		return
	}
	utils.RespondSuccess(c, http.StatusOK, "Mutasi saldo nasabah berhasil diambil", res)
}

// respondError memetakan error domain ke HTTP status
func (h *Handler) respondError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrNasabahNotFound):
		utils.RespondError(c, http.StatusNotFound, "Nasabah tidak ditemukan")
	case errors.Is(err, ErrPeriodConflict):
		utils.RespondValidationError(c, map[string]string{"month": "Gunakan filter bulan ATAU rentang tanggal, tidak keduanya"})
	case errors.Is(err, ErrInvalidDateRange):
		utils.RespondValidationError(c, map[string]string{"date_from": "Tanggal awal tidak boleh setelah tanggal akhir"})
	default:
		utils.RespondInternal(c, err)
	}
}
