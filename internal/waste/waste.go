// Package waste menangani master data sampah: kategori (category.go),
// jenis sampah (type.go), dan harga dinamis (Task 2.4).
package waste

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/banksampahku/backend/internal/audit"
	"github.com/banksampahku/backend/internal/middleware"
	"github.com/banksampahku/backend/internal/utils"
)

// Handler menangani seluruh endpoint HTTP modul waste
type Handler struct {
	categories *CategoryService
	types      *TypeService
	prices     *PriceService
	audit      audit.Recorder
}

// NewHandler membuat handler modul waste
func NewHandler(categories *CategoryService, types *TypeService, prices *PriceService, rec audit.Recorder) *Handler {
	return &Handler{categories: categories, types: types, prices: prices, audit: rec}
}

// RegisterAdminRoutes mendaftarkan route ke group /api/v1/admin (RequireAuth + RequireRole admin)
func (h *Handler) RegisterAdminRoutes(admin *gin.RouterGroup) {
	categoryID := middleware.ValidUUIDParam("id", "Kategori sampah tidak ditemukan")

	cat := admin.Group("/waste-categories")
	cat.POST("", middleware.Audit(h.audit, audit.ActionCategoryCreate, audit.EntityCategory), h.CreateCategory)
	cat.GET("", h.ListCategories)
	cat.PATCH("/:id", categoryID, middleware.Audit(h.audit, audit.ActionCategoryUpdate, audit.EntityCategory), h.UpdateCategory)
	cat.DELETE("/:id", categoryID, middleware.Audit(h.audit, audit.ActionCategoryDeactivate, audit.EntityCategory), h.DeactivateCategory)

	typeID := middleware.ValidUUIDParam("id", "Jenis sampah tidak ditemukan")

	typ := admin.Group("/waste-types")
	typ.POST("", middleware.Audit(h.audit, audit.ActionWasteTypeCreate, audit.EntityWasteType), h.CreateType)
	typ.GET("", h.ListTypes)
	typ.GET("/:id", typeID, h.GetType)
	typ.PATCH("/:id", typeID, middleware.Audit(h.audit, audit.ActionWasteTypeUpdate, audit.EntityWasteType), h.UpdateType)
	typ.DELETE("/:id", typeID, middleware.Audit(h.audit, audit.ActionWasteTypeDeactivate, audit.EntityWasteType), h.DeactivateType)

	// Harga dinamis per jenis sampah
	typ.POST("/:id/prices", typeID, middleware.Audit(h.audit, audit.ActionPriceUpdate, audit.EntityPrice), h.CreatePrice)
	typ.GET("/:id/prices", typeID, h.ListPrices)
	typ.GET("/:id/price-active", typeID, h.ActivePrice)
	typ.DELETE("/:id/prices/:priceId", typeID,
		middleware.ValidUUIDParam("priceId", "Harga tidak ditemukan"),
		middleware.Audit(h.audit, audit.ActionPriceCancel, audit.EntityPrice), h.CancelPrice)
}

// RegisterPublicRoutes mendaftarkan route publik (tanpa login) ke group /api/v1
func (h *Handler) RegisterPublicRoutes(api *gin.RouterGroup) {
	api.GET("/waste-categories", h.ListPublicCategories)
}

// respondError memetakan error domain ke HTTP status
func (h *Handler) respondError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrCategoryNotFound):
		utils.RespondError(c, http.StatusNotFound, "Kategori sampah tidak ditemukan")
	case errors.Is(err, ErrSchoolNotFound):
		utils.RespondError(c, http.StatusNotFound, "Sekolah tidak ditemukan")
	case errors.Is(err, ErrCategoryNameTaken):
		utils.RespondValidationError(c, map[string]string{"name": "Nama kategori sudah digunakan"})
	case errors.Is(err, ErrCategoryAlreadyInactive):
		utils.RespondError(c, http.StatusConflict, "Kategori sampah sudah dalam status nonaktif")
	case errors.Is(err, ErrTypeNotFound):
		utils.RespondError(c, http.StatusNotFound, "Jenis sampah tidak ditemukan")
	case errors.Is(err, ErrTypeNameTaken):
		utils.RespondValidationError(c, map[string]string{"name": "Nama jenis sampah sudah digunakan di kategori ini"})
	case errors.Is(err, ErrTypeAlreadyInactive):
		utils.RespondError(c, http.StatusConflict, "Jenis sampah sudah dalam status nonaktif")
	case errors.Is(err, ErrCategoryInvalid):
		utils.RespondValidationError(c, map[string]string{"category_id": "Kategori tidak ditemukan"})
	case errors.Is(err, ErrCategoryInactive):
		utils.RespondValidationError(c, map[string]string{
			"category_id": "Kategori sedang nonaktif, aktifkan kategori terlebih dahulu",
		})
	case errors.Is(err, ErrPriceDateTaken):
		utils.RespondError(c, http.StatusConflict,
			"Harga untuk tanggal tersebut sudah ada. Pilih tanggal berlaku lain untuk mengubah harga")
	case errors.Is(err, ErrPriceDateTooOld):
		utils.RespondValidationError(c, map[string]string{
			"effective_date": fmt.Sprintf("Tanggal berlaku maksimal %d hari ke belakang", MaxPriceBackdateDays),
		})
	case errors.Is(err, ErrPriceDateTooFar):
		utils.RespondValidationError(c, map[string]string{
			"effective_date": fmt.Sprintf("Tanggal berlaku maksimal %d hari ke depan", MaxPriceScheduleDays),
		})
	case errors.Is(err, ErrInvalidDateFormat):
		utils.RespondValidationError(c, map[string]string{"date": "Format tanggal harus YYYY-MM-DD"})
	case errors.Is(err, ErrPriceNotFound):
		utils.RespondError(c, http.StatusNotFound, "Harga tidak ditemukan")
	case errors.Is(err, ErrPriceNotScheduled):
		utils.RespondError(c, http.StatusConflict,
			"Hanya harga terjadwal (belum berlaku) yang bisa dibatalkan. Untuk mengubah harga yang sudah berlaku, tambahkan harga baru")
	case errors.Is(err, ErrNoActivePrice):
		utils.RespondError(c, http.StatusNotFound, "Belum ada harga yang berlaku pada tanggal tersebut")
	case errors.Is(err, ErrNoChanges):
		utils.RespondError(c, http.StatusBadRequest, "Tidak ada data yang diubah")
	default:
		utils.RespondInternal(c, err)
	}
}

// DateLayout adalah format tanggal API (YYYY-MM-DD)
const DateLayout = "2006-01-02"

// Today mengembalikan tanggal hari ini (YYYY-MM-DD) menurut zona waktu aplikasi
// (time.Local diatur dari APP_TIMEZONE di main.go)
func Today() string {
	return time.Now().Format(DateLayout)
}

// trimPtr merapikan string opsional; string kosong dianggap nil (kolom dikosongkan)
func trimPtr(s *string) *string {
	if s == nil {
		return nil
	}
	v := strings.TrimSpace(*s)
	if v == "" {
		return nil
	}
	return &v
}

func equalPtr(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
