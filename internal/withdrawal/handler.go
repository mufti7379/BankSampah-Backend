package withdrawal

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/banksampahku/backend/internal/auth"
	"github.com/banksampahku/backend/internal/middleware"
	"github.com/banksampahku/backend/internal/utils"
)

// Handler menangani endpoint penarikan saldo untuk nasabah & admin.
// Audit log ditulis oleh service di dalam DB transaction (bukan middleware Audit).
type Handler struct {
	service *Service
}

// NewHandler membuat handler penarikan
func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// RegisterNasabahRoutes mendaftarkan route ke group /api/v1/nasabah
func (h *Handler) RegisterNasabahRoutes(nasabah *gin.RouterGroup) {
	validID := middleware.ValidUUIDParam("id", "Pengajuan penarikan tidak ditemukan")
	rg := nasabah.Group("/me/withdrawals")
	rg.POST("", h.Create)
	rg.GET("", h.ListMine)
	rg.GET("/:id", validID, h.DetailMine)
	rg.PATCH("/:id/cancel", validID, h.Cancel)
}

// RegisterAdminRoutes mendaftarkan route ke group /api/v1/admin
func (h *Handler) RegisterAdminRoutes(admin *gin.RouterGroup) {
	validID := middleware.ValidUUIDParam("id", "Pengajuan penarikan tidak ditemukan")
	rg := admin.Group("/withdrawals")
	rg.GET("", h.ListAdmin)
	rg.GET("/:id", validID, h.DetailAdmin)
	rg.PATCH("/:id/approve", validID, h.Approve)
	rg.PATCH("/:id/reject", validID, h.Reject)
	rg.PATCH("/:id/complete", validID, h.Complete)
}

// ---------- Nasabah ----------

// Create: POST /api/v1/nasabah/me/withdrawals
func (h *Handler) Create(c *gin.Context) {
	var req CreateRequest
	if !utils.BindJSON(c, &req) {
		return
	}
	item, err := h.service.Create(c.Request.Context(), auth.CurrentUser(c), req)
	if err != nil {
		h.respondError(c, err)
		return
	}
	utils.RespondSuccess(c, http.StatusCreated,
		"Pengajuan penarikan "+utils.FormatRupiah(item.Amount)+" berhasil dikirim dan sedang dalam verifikasi admin", item)
}

// ListMine: GET /api/v1/nasabah/me/withdrawals?status=&page=&limit=
func (h *Handler) ListMine(c *gin.Context) {
	var q MyListQuery
	if !utils.BindQuery(c, &q) {
		return
	}
	q.Normalize()
	items, total, err := h.service.ListMine(c.Request.Context(), auth.CurrentUser(c), q)
	if err != nil {
		h.respondError(c, err)
		return
	}
	utils.RespondSuccess(c, http.StatusOK, "Riwayat penarikan berhasil diambil",
		utils.PaginatedData[Item]{Items: items, Meta: utils.NewPageMeta(q.PageQuery, total)})
}

// DetailMine: GET /api/v1/nasabah/me/withdrawals/:id
func (h *Handler) DetailMine(c *gin.Context) {
	item, err := h.service.GetMine(c.Request.Context(), auth.CurrentUser(c), c.Param("id"))
	if err != nil {
		h.respondError(c, err)
		return
	}
	utils.RespondSuccess(c, http.StatusOK, "Detail penarikan berhasil diambil", item)
}

// Cancel: PATCH /api/v1/nasabah/me/withdrawals/:id/cancel
func (h *Handler) Cancel(c *gin.Context) {
	item, err := h.service.Cancel(c.Request.Context(), auth.CurrentUser(c), c.Param("id"))
	if err != nil {
		h.respondError(c, err)
		return
	}
	utils.RespondSuccess(c, http.StatusOK, "Pengajuan penarikan berhasil dibatalkan", item)
}

// ---------- Admin ----------

// ListAdmin: GET /api/v1/admin/withdrawals?status=pending&nasabah_id=&search=&date_from=&date_to=
func (h *Handler) ListAdmin(c *gin.Context) {
	var q AdminListQuery
	if !utils.BindQuery(c, &q) {
		return
	}
	q.Normalize()
	res, err := h.service.ListAdmin(c.Request.Context(), auth.CurrentUser(c), q)
	if err != nil {
		h.respondError(c, err)
		return
	}
	utils.RespondSuccess(c, http.StatusOK, "Daftar pengajuan penarikan berhasil diambil", res)
}

// DetailAdmin: GET /api/v1/admin/withdrawals/:id
func (h *Handler) DetailAdmin(c *gin.Context) {
	item, err := h.service.GetAdmin(c.Request.Context(), auth.CurrentUser(c), c.Param("id"))
	if err != nil {
		h.respondError(c, err)
		return
	}
	utils.RespondSuccess(c, http.StatusOK, "Detail pengajuan penarikan berhasil diambil", item)
}

// Approve: PATCH /api/v1/admin/withdrawals/:id/approve  body: {"note": "..."} (opsional)
func (h *Handler) Approve(c *gin.Context) {
	var req ApproveRequest
	if !bindOptionalJSON(c, &req) {
		return
	}
	item, err := h.service.Approve(c.Request.Context(), auth.CurrentUser(c), c.Param("id"), req, c.ClientIP())
	if err != nil {
		h.respondError(c, err)
		return
	}
	utils.RespondSuccess(c, http.StatusOK, "Pengajuan disetujui. Serahkan uang tunai lalu tandai sebagai selesai", item)
}

// Reject: PATCH /api/v1/admin/withdrawals/:id/reject  body: {"note": "alasan"} (wajib)
func (h *Handler) Reject(c *gin.Context) {
	var req RejectRequest
	if !utils.BindJSON(c, &req) {
		return
	}
	item, err := h.service.Reject(c.Request.Context(), auth.CurrentUser(c), c.Param("id"), req, c.ClientIP())
	if err != nil {
		h.respondError(c, err)
		return
	}
	utils.RespondSuccess(c, http.StatusOK, "Pengajuan penarikan ditolak", item)
}

// Complete: PATCH /api/v1/admin/withdrawals/:id/complete  body: {"note": "..."} (opsional)
func (h *Handler) Complete(c *gin.Context) {
	var req ApproveRequest
	if !bindOptionalJSON(c, &req) {
		return
	}
	item, err := h.service.Complete(c.Request.Context(), auth.CurrentUser(c), c.Param("id"), req, c.ClientIP())
	if err != nil {
		h.respondError(c, err)
		return
	}
	utils.RespondSuccess(c, http.StatusOK,
		"Penarikan selesai. Saldo nasabah berkurang "+utils.FormatRupiah(item.Amount), item)
}

// bindOptionalJSON mengizinkan body kosong (catatan opsional)
func bindOptionalJSON(c *gin.Context, req any) bool {
	if c.Request.ContentLength == 0 {
		return true
	}
	return utils.BindJSON(c, req)
}

// respondError memetakan error domain ke HTTP status
func (h *Handler) respondError(c *gin.Context, err error) {
	var insufficient *InsufficientBalanceError
	var transition *InvalidTransitionError
	switch {
	case errors.As(err, &insufficient):
		msg := "Saldo tidak mencukupi. Saldo Anda saat ini " + utils.FormatRupiah(insufficient.Balance)
		if insufficient.ForAdmin {
			msg = "Saldo nasabah tidak mencukupi (saldo saat ini " + utils.FormatRupiah(insufficient.Balance) +
				"). Tolak pengajuan ini atau periksa mutasi saldo nasabah"
		}
		utils.RespondValidationError(c, map[string]string{"amount": msg})
	case errors.As(err, &transition):
		utils.RespondError(c, http.StatusConflict,
			"Pengajuan ini berstatus \""+statusLabels[transition.Current]+"\" sehingga tidak bisa di"+transition.Action)
	case errors.Is(err, ErrActiveRequestExists):
		utils.RespondError(c, http.StatusConflict,
			"Anda masih memiliki pengajuan penarikan yang sedang diproses. Tunggu hingga selesai atau batalkan terlebih dahulu")
	case errors.Is(err, ErrNotFound):
		utils.RespondError(c, http.StatusNotFound, "Pengajuan penarikan tidak ditemukan")
	case errors.Is(err, ErrNasabahNotActive):
		utils.RespondError(c, http.StatusForbidden, "Akun Anda belum aktif atau sudah dinonaktifkan")
	case errors.Is(err, ErrInvalidDateRange):
		utils.RespondValidationError(c, map[string]string{"date_from": "Tanggal awal tidak boleh setelah tanggal akhir"})
	default:
		utils.RespondInternal(c, err)
	}
}
