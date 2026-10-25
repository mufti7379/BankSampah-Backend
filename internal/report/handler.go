package report

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"

	"github.com/banksampahku/backend/internal/auth"
	"github.com/banksampahku/backend/internal/utils"
)

// Handler menangani endpoint laporan admin
type Handler struct {
	service *Service
}

// NewHandler membuat handler laporan
func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// RegisterRoutes mendaftarkan route ke group /api/v1/admin (RequireAuth + RequireRole admin)
func (h *Handler) RegisterRoutes(admin *gin.RouterGroup) {
	rg := admin.Group("/reports")
	rg.GET("/summary", h.Summary)
	rg.GET("/by-category", h.ByCategory)
	rg.GET("/by-nasabah", h.ByNasabah)
	rg.GET("/export.csv", h.Export)
}

// Summary: GET /api/v1/admin/reports/summary?from=&to=
func (h *Handler) Summary(c *gin.Context) {
	var q PeriodQuery
	if !utils.BindQuery(c, &q) {
		return
	}
	res, err := h.service.Summary(c.Request.Context(), auth.CurrentUser(c), q)
	if err != nil {
		h.respondError(c, err)
		return
	}
	utils.RespondSuccess(c, http.StatusOK, "Ringkasan laporan berhasil diambil", res)
}

// ByCategory: GET /api/v1/admin/reports/by-category?from=&to=
func (h *Handler) ByCategory(c *gin.Context) {
	var q PeriodQuery
	if !utils.BindQuery(c, &q) {
		return
	}
	res, err := h.service.ByCategory(c.Request.Context(), auth.CurrentUser(c), q)
	if err != nil {
		h.respondError(c, err)
		return
	}
	utils.RespondSuccess(c, http.StatusOK, "Laporan per kategori berhasil diambil", res)
}

// ByNasabah: GET /api/v1/admin/reports/by-nasabah?from=&to=&search=&sort=&all=&page=&limit=
func (h *Handler) ByNasabah(c *gin.Context) {
	var q ByNasabahQuery
	if !utils.BindQuery(c, &q) {
		return
	}
	q.Normalize()
	res, err := h.service.ByNasabah(c.Request.Context(), auth.CurrentUser(c), q)
	if err != nil {
		h.respondError(c, err)
		return
	}
	utils.RespondSuccess(c, http.StatusOK, "Laporan per nasabah berhasil diambil", res)
}

// Export: GET /api/v1/admin/reports/export.csv?type=&from=&to=&delimiter=
func (h *Handler) Export(c *gin.Context) {
	var q ExportQuery
	if !utils.BindQuery(c, &q) {
		return
	}
	job, err := h.service.Export(c.Request.Context(), auth.CurrentUser(c), q)
	if err != nil {
		h.respondError(c, err)
		return
	}

	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, job.Filename))
	c.Header("Cache-Control", "no-store")
	c.Status(http.StatusOK)
	if err := job.Write(c.Writer); err != nil {
		// Header sudah terkirim sehingga status tidak bisa diubah lagi; cukup dicatat
		log.Error().Err(err).Str("file", job.Filename).Msg("Ekspor CSV terhenti di tengah jalan")
	}
}

func (h *Handler) respondError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrInvalidRange):
		utils.RespondValidationError(c, map[string]string{"from": "Tanggal awal tidak boleh setelah tanggal akhir"})
	case errors.Is(err, ErrRangeTooLong):
		utils.RespondValidationError(c, map[string]string{"to": fmt.Sprintf("Rentang laporan maksimal %d hari", MaxRangeDays)})
	default:
		utils.RespondInternal(c, err)
	}
}
