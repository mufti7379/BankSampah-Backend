package audit

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/banksampahku/backend/internal/auth"
	"github.com/banksampahku/backend/internal/utils"
)

// Handler menangani endpoint baca log aktivitas admin.
// Catatan: paket ini tidak boleh mengimpor "middleware" (middleware sudah mengimpor audit).
type Handler struct {
	repo *Repository
}

// NewHandler membuat handler log aktivitas
func NewHandler(repo *Repository) *Handler {
	return &Handler{repo: repo}
}

// RegisterRoutes mendaftarkan route ke group /api/v1/admin (RequireAuth + RequireRole admin).
// Sengaja TIDAK ada endpoint ubah/hapus: log aktivitas tidak boleh dimanipulasi (PRD Bab 6H).
func (h *Handler) RegisterRoutes(admin *gin.RouterGroup) {
	rg := admin.Group("/audit-logs")
	rg.GET("", h.List)
	rg.GET("/actions", h.Actions)
}

// List: GET /api/v1/admin/audit-logs?actor=&action=&entity=&entity_id=&from=&to=&page=&limit=
func (h *Handler) List(c *gin.Context) {
	var q ListQuery
	if !utils.BindQuery(c, &q) {
		return
	}
	q.Normalize()

	items, total, err := h.repo.List(c.Request.Context(), auth.CurrentUser(c).SchoolID, q)
	if err != nil {
		if errors.Is(err, ErrInvalidDateRange) {
			utils.RespondValidationError(c, map[string]string{"from": "Tanggal awal tidak boleh setelah tanggal akhir"})
			return
		}
		utils.RespondInternal(c, err)
		return
	}
	utils.RespondSuccess(c, http.StatusOK, "Log aktivitas berhasil diambil",
		utils.PaginatedData[LogItem]{Items: items, Meta: utils.NewPageMeta(q.PageQuery, total)})
}

// Actions: GET /api/v1/admin/audit-logs/actions (pilihan filter aksi + labelnya)
func (h *Handler) Actions(c *gin.Context) {
	utils.RespondSuccess(c, http.StatusOK, "Daftar jenis aktivitas", ActionOptions())
}
