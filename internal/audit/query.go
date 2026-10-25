package audit

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/banksampahku/backend/internal/utils"
)

// actionLabels adalah label ramah pengguna untuk halaman Log Aktivitas Admin
var actionLabels = map[string]string{
	ActionWeighingCreate:      "Mencatat penimbangan",
	ActionPriceUpdate:         "Mengatur harga sampah",
	ActionPriceCancel:         "Membatalkan harga terjadwal",
	ActionNasabahCreate:       "Menambah nasabah",
	ActionNasabahUpdate:       "Mengubah data nasabah",
	ActionNasabahApprove:      "Menyetujui pendaftaran nasabah",
	ActionUserDeactivate:      "Menonaktifkan nasabah",
	ActionUserActivate:        "Mengaktifkan kembali nasabah",
	ActionCategoryCreate:      "Menambah kategori sampah",
	ActionCategoryUpdate:      "Mengubah kategori sampah",
	ActionCategoryDeactivate:  "Menonaktifkan kategori sampah",
	ActionWasteTypeCreate:     "Menambah jenis sampah",
	ActionWasteTypeUpdate:     "Mengubah jenis sampah",
	ActionWasteTypeDeactivate: "Menonaktifkan jenis sampah",
	ActionWithdrawalApprove:   "Menyetujui penarikan saldo",
	ActionWithdrawalReject:    "Menolak penarikan saldo",
	ActionWithdrawalComplete:  "Menyelesaikan penarikan (uang diserahkan)",
	ActionSettingsUpdate:      "Mengubah pengaturan",
}

// ErrInvalidDateRange dikembalikan jika from > to
var ErrInvalidDateRange = errors.New("tanggal 'from' tidak boleh setelah 'to'")

// ---------- Request / Response ----------

// ListQuery adalah query GET /api/v1/admin/audit-logs
type ListQuery struct {
	utils.PageQuery
	Actor    string `form:"actor" binding:"omitempty,max=100"` // ID admin (UUID) atau potongan nama
	Action   string `form:"action" binding:"omitempty,max=50"`
	Entity   string `form:"entity" binding:"omitempty,max=50"`
	EntityID string `form:"entity_id" binding:"omitempty,uuid"`
	From     string `form:"from" binding:"omitempty,datetime=2006-01-02"`
	To       string `form:"to" binding:"omitempty,datetime=2006-01-02"`
}

// ActorRef adalah admin pelaku aksi
type ActorRef struct {
	ID       string `json:"id"`
	FullName string `json:"full_name"`
	Role     string `json:"role"`
}

// LogItem adalah satu baris log aktivitas
type LogItem struct {
	ID          string          `json:"id"`
	Action      string          `json:"action"`
	ActionLabel string          `json:"action_label"`
	Entity      string          `json:"entity"`
	EntityID    *string         `json:"entity_id"`
	Actor       *ActorRef       `json:"actor"`
	IPAddress   *string         `json:"ip_address"`
	Metadata    json.RawMessage `json:"metadata"` // payload before/after (JSONB apa adanya)
	CreatedAt   time.Time       `json:"created_at"`
}

// ActionOption adalah satu pilihan filter aksi (untuk dropdown)
type ActionOption struct {
	Code  string `json:"code"`
	Label string `json:"label"`
}

// ---------- Query ----------

type logRow struct {
	ID        string    `gorm:"column:id"`
	Action    string    `gorm:"column:action"`
	Entity    string    `gorm:"column:entity"`
	EntityID  *string   `gorm:"column:entity_id"`
	IPAddress *string   `gorm:"column:ip_address"`
	Metadata  *string   `gorm:"column:metadata"`
	CreatedAt time.Time `gorm:"column:created_at"`
	ActorID   *string   `gorm:"column:actor_id"`
	ActorName *string   `gorm:"column:actor_name"`
	ActorRole *string   `gorm:"column:actor_role"`
}

func (r logRow) toItem() LogItem {
	it := LogItem{
		ID: r.ID, Action: r.Action, ActionLabel: actionLabel(r.Action), Entity: r.Entity,
		EntityID: r.EntityID, IPAddress: r.IPAddress, CreatedAt: r.CreatedAt,
		Metadata: json.RawMessage("null"),
	}
	if r.Metadata != nil {
		it.Metadata = json.RawMessage(*r.Metadata)
	}
	if r.ActorID != nil && r.ActorName != nil {
		role := ""
		if r.ActorRole != nil {
			role = *r.ActorRole
		}
		it.Actor = &ActorRef{ID: *r.ActorID, FullName: *r.ActorName, Role: role}
	}
	return it
}

// List mengambil log aktivitas (terbaru di atas) milik admin-admin di sekolah yang sama.
// Log hanya bisa DIBACA - tidak ada fungsi ubah/hapus (PRD Bab 6H).
func (r *Repository) List(ctx context.Context, schoolID *string, q ListQuery) ([]LogItem, int64, error) {
	base := r.db.WithContext(ctx).Table("audit_logs AS l").
		Joins("JOIN users a ON a.id = l.actor_id").
		Where("a.school_id IS NOT DISTINCT FROM ?", schoolID)

	if actor := strings.TrimSpace(q.Actor); actor != "" {
		if utils.IsUUID(actor) {
			base = base.Where("l.actor_id = ?", actor)
		} else {
			base = base.Where("a.full_name ILIKE ?", "%"+utils.EscapeLike(actor)+"%")
		}
	}
	if q.Action != "" {
		base = base.Where("l.action = ?", strings.ToUpper(q.Action))
	}
	if q.Entity != "" {
		base = base.Where("l.entity = ?", q.Entity)
	}
	if q.EntityID != "" {
		base = base.Where("l.entity_id = ?", q.EntityID)
	}
	if q.From != "" || q.To != "" {
		from, to, err := dateRange(q.From, q.To)
		if err != nil {
			return nil, 0, err
		}
		if from != nil {
			base = base.Where("l.created_at >= ?", *from)
		}
		if to != nil {
			base = base.Where("l.created_at < ?", *to)
		}
	}

	var total int64
	if err := base.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var rows []logRow
	err := base.Select(`l.id, l.action, l.entity, l.entity_id, l.ip_address, l.metadata::text AS metadata,
			l.created_at, a.id AS actor_id, a.full_name AS actor_name, a.role AS actor_role`).
		Order("l.created_at DESC, l.id DESC").
		Limit(q.Limit).Offset(q.Offset()).
		Scan(&rows).Error
	if err != nil {
		return nil, 0, err
	}

	items := make([]LogItem, len(rows))
	for i, rw := range rows {
		items[i] = rw.toItem()
	}
	return items, total, nil
}

// dateRange mengubah from/to (YYYY-MM-DD, inklusif) menjadi rentang waktu [from, to+1)
// menurut zona waktu aplikasi
func dateRange(fromStr, toStr string) (*time.Time, *time.Time, error) {
	const layout = "2006-01-02"
	if fromStr != "" && toStr != "" && fromStr > toStr {
		return nil, nil, ErrInvalidDateRange
	}
	var from, to *time.Time
	if fromStr != "" {
		t, _ := time.ParseInLocation(layout, fromStr, time.Local)
		from = &t
	}
	if toStr != "" {
		t, _ := time.ParseInLocation(layout, toStr, time.Local)
		t = t.AddDate(0, 0, 1)
		to = &t
	}
	return from, to, nil
}

// actionLabel mengembalikan label aksi, atau kode aslinya jika belum punya label
func actionLabel(code string) string {
	if l, ok := actionLabels[code]; ok {
		return l
	}
	return code
}

// ActionOptions mengembalikan daftar aksi yang dikenal (urut abjad label) untuk dropdown filter
func ActionOptions() []ActionOption {
	out := make([]ActionOption, 0, len(actionLabels))
	for code, label := range actionLabels {
		out = append(out, ActionOption{Code: code, Label: label})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Label < out[j].Label })
	return out
}
