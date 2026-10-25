// Package audit mencatat log aktivitas admin (audit trail) ke tabel audit_logs.
// Log bersifat append-only: tidak ada fungsi update/delete (PRD Bab 6H).
package audit

import (
	"context"
	"encoding/json"
	"fmt"

	"gorm.io/gorm"
)

// Daftar aksi audit (PRD Bab 6H & Task 4.2)
const (
	ActionWeighingCreate      = "WEIGHING_CREATE"
	ActionPriceUpdate         = "PRICE_UPDATE"
	ActionPriceCancel         = "PRICE_CANCEL"
	ActionNasabahCreate       = "NASABAH_CREATE"
	ActionNasabahUpdate       = "NASABAH_UPDATE"
	ActionNasabahApprove      = "NASABAH_APPROVE"
	ActionUserDeactivate      = "USER_DEACTIVATE"
	ActionUserActivate        = "USER_ACTIVATE"
	ActionCategoryCreate      = "CATEGORY_CREATE"
	ActionCategoryUpdate      = "CATEGORY_UPDATE"
	ActionCategoryDeactivate  = "CATEGORY_DEACTIVATE"
	ActionWasteTypeCreate     = "WASTE_TYPE_CREATE"
	ActionWasteTypeUpdate     = "WASTE_TYPE_UPDATE"
	ActionWasteTypeDeactivate = "WASTE_TYPE_DEACTIVATE"
	ActionWithdrawalApprove   = "WITHDRAWAL_APPROVE"
	ActionWithdrawalReject    = "WITHDRAWAL_REJECT"
	ActionWithdrawalComplete  = "WITHDRAWAL_COMPLETE"
	ActionSettingsUpdate      = "SETTINGS_UPDATE"
)

// Daftar entitas yang diaudit (kolom audit_logs.entity)
const (
	EntityWeighing   = "weighing"
	EntityPrice      = "price_history"
	EntityUser       = "user"
	EntityCategory   = "waste_category"
	EntityWasteType  = "waste_type"
	EntityWithdrawal = "withdrawal_request"
	EntitySchool     = "school"
)

// Entry adalah satu baris log aktivitas
type Entry struct {
	ActorID   string         // ID admin yang melakukan aksi
	Action    string         // misal WEIGHING_CREATE
	Entity    string         // misal weighing
	EntityID  string         // UUID entitas (boleh kosong)
	Metadata  map[string]any // disimpan sebagai JSONB (payload before/after, dll.)
	IPAddress string
}

// Recorder adalah kontrak penyimpan log audit (memudahkan testing)
type Recorder interface {
	Record(ctx context.Context, e Entry) error
}

// Repository menyimpan log audit ke PostgreSQL
type Repository struct {
	db *gorm.DB
}

// NewRepository membuat repository audit
func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

// Record menyimpan satu log audit
func (r *Repository) Record(ctx context.Context, e Entry) error {
	return RecordTx(r.db.WithContext(ctx), e)
}

// RecordTx menyimpan log audit memakai koneksi/transaksi yang diberikan.
// Gunakan ini di dalam DB transaction bisnis (misal penimbangan) agar log
// ikut ter-rollback jika transaksi gagal.
func RecordTx(tx *gorm.DB, e Entry) error {
	var metadata any
	if len(e.Metadata) > 0 {
		b, err := json.Marshal(e.Metadata)
		if err != nil {
			return fmt.Errorf("gagal serialisasi metadata audit: %w", err)
		}
		metadata = string(b)
	}

	return tx.Exec(`
		INSERT INTO audit_logs (actor_id, action, entity, entity_id, metadata, ip_address)
		VALUES (NULLIF(?, '')::uuid, ?, ?, NULLIF(?, '')::uuid, ?::jsonb, NULLIF(?, ''))`,
		e.ActorID, e.Action, e.Entity, e.EntityID, metadata, e.IPAddress,
	).Error
}
