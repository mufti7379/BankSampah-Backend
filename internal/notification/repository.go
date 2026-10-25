package notification

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
)

// ErrNotFound dikembalikan jika notifikasi tidak ada atau bukan milik user
var ErrNotFound = errors.New("notifikasi tidak ditemukan")

// Repository menangani query notifikasi. SETIAP query dibatasi user_id pemilik,
// sehingga user hanya bisa melihat/mengubah notifikasinya sendiri (PRD Bab 6G).
type Repository struct {
	db *gorm.DB
}

// NewRepository membuat repository notifikasi
func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

type row struct {
	ID          string    `gorm:"column:id"`
	Type        string    `gorm:"column:type"`
	Title       string    `gorm:"column:title"`
	Body        string    `gorm:"column:body"`
	ReferenceID *string   `gorm:"column:reference_id"`
	IsRead      bool      `gorm:"column:is_read"`
	CreatedAt   time.Time `gorm:"column:created_at"`
}

func (r row) toItem() Item {
	return Item{
		ID: r.ID, Type: r.Type, Title: r.Title, Body: r.Body, Target: targets[r.Type],
		ReferenceID: r.ReferenceID, IsRead: r.IsRead, CreatedAt: r.CreatedAt,
	}
}

func (r *Repository) base(ctx context.Context, userID string, q ListQuery) *gorm.DB {
	db := r.db.WithContext(ctx).Table("notifications").Where("user_id = ?", userID)
	switch q.Status {
	case "unread":
		db = db.Where("NOT is_read")
	case "read":
		db = db.Where("is_read")
	}
	if q.Type != "" {
		db = db.Where("type = ?", q.Type)
	}
	return db
}

// List mengambil notifikasi milik user (terbaru di atas) dengan pagination
func (r *Repository) List(ctx context.Context, userID string, q ListQuery) ([]Item, int64, error) {
	var total int64
	if err := r.base(ctx, userID, q).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var rows []row
	err := r.base(ctx, userID, q).
		Select("id, type, title, body, reference_id, is_read, created_at").
		Order("created_at DESC, id DESC").
		Limit(q.Limit).Offset(q.Offset()).
		Scan(&rows).Error
	if err != nil {
		return nil, 0, err
	}

	items := make([]Item, len(rows))
	for i, rw := range rows {
		items[i] = rw.toItem()
	}
	return items, total, nil
}

// UnreadCount menghitung notifikasi yang belum dibaca milik user
func (r *Repository) UnreadCount(ctx context.Context, userID string) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Table("notifications").
		Where("user_id = ? AND NOT is_read", userID).Count(&n).Error
	return n, err
}

// MarkRead menandai satu notifikasi milik user sebagai dibaca.
// Idempoten: notifikasi yang sudah dibaca tetap dianggap sukses.
func (r *Repository) MarkRead(ctx context.Context, userID, id string) (*Item, error) {
	var rows []row
	err := r.db.WithContext(ctx).Raw(`
		UPDATE notifications SET is_read = TRUE
		WHERE id = ? AND user_id = ?
		RETURNING id, type, title, body, reference_id, is_read, created_at`,
		id, userID,
	).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, ErrNotFound
	}
	it := rows[0].toItem()
	return &it, nil
}

// MarkAllRead menandai semua notifikasi user yang belum dibaca; mengembalikan jumlah yang berubah
func (r *Repository) MarkAllRead(ctx context.Context, userID string) (int64, error) {
	res := r.db.WithContext(ctx).Exec(`UPDATE notifications SET is_read = TRUE WHERE user_id = ? AND NOT is_read`, userID)
	return res.RowsAffected, res.Error
}
