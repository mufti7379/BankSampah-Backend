package mutation

import (
	"context"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/banksampahku/backend/internal/utils"
)

// Repository menangani query baca balance_mutations (penulisan lewat ledger.go)
type Repository struct {
	db *gorm.DB
}

// NewRepository membuat repository mutasi
func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

// filter adalah kriteria pencarian mutasi yang sudah dinormalisasi
type filter struct {
	SchoolID      *string
	NasabahID     string
	Search        string
	Type          string
	ReferenceType string
	From          *time.Time // inklusif
	To            *time.Time // eksklusif
}

func (r *Repository) base(ctx context.Context, f filter) *gorm.DB {
	q := r.db.WithContext(ctx).Table("balance_mutations AS m").
		Joins("JOIN users n ON n.id = m.nasabah_id").
		Where("n.school_id IS NOT DISTINCT FROM ?", f.SchoolID)
	if f.NasabahID != "" {
		q = q.Where("m.nasabah_id = ?", f.NasabahID)
	}
	if s := strings.TrimSpace(f.Search); s != "" {
		like := "%" + utils.EscapeLike(s) + "%"
		q = q.Where("(n.full_name ILIKE ? OR n.qr_code ILIKE ?)", like, like)
	}
	if f.Type != "" {
		q = q.Where("m.type = ?", f.Type)
	}
	if f.ReferenceType != "" {
		q = q.Where("m.reference_type = ?", f.ReferenceType)
	}
	if f.From != nil {
		q = q.Where("m.created_at >= ?", *f.From)
	}
	if f.To != nil {
		q = q.Where("m.created_at < ?", *f.To)
	}
	return q
}

type itemRow struct {
	ID            string    `gorm:"column:id"`
	Type          string    `gorm:"column:type"`
	Amount        int64     `gorm:"column:amount"`
	BalanceAfter  int64     `gorm:"column:balance_after"`
	ReferenceType string    `gorm:"column:reference_type"`
	ReferenceID   *string   `gorm:"column:reference_id"`
	Description   *string   `gorm:"column:description"`
	CreatedAt     time.Time `gorm:"column:created_at"`
	NasabahID     string    `gorm:"column:nasabah_id"`
	NasabahName   string    `gorm:"column:nasabah_name"`
	NasabahClass  *string   `gorm:"column:nasabah_class"`
	NasabahQR     *string   `gorm:"column:nasabah_qr"`
}

func (r itemRow) toItem(withNasabah bool) Item {
	it := Item{
		ID: r.ID, Type: r.Type, Label: typeLabels[r.Type], Amount: r.Amount, BalanceAfter: r.BalanceAfter,
		ReferenceType: r.ReferenceType, SourceLabel: sourceLabels[r.ReferenceType],
		ReferenceID: r.ReferenceID, Description: r.Description, CreatedAt: r.CreatedAt,
	}
	if withNasabah {
		it.Nasabah = &NasabahRef{ID: r.NasabahID, FullName: r.NasabahName, ClassName: r.NasabahClass, QRCode: r.NasabahQR}
	}
	return it
}

// List mengambil mutasi terbaru di atas + ringkasan total sesuai filter
func (r *Repository) List(ctx context.Context, f filter, page utils.PageQuery, withNasabah bool) (*ListResponse, error) {
	var sum Summary
	err := r.base(ctx, f).Select(`
		COALESCE(SUM(CASE WHEN m.type = 'credit' THEN m.amount ELSE 0 END), 0) AS total_credit,
		COALESCE(SUM(CASE WHEN m.type = 'debit'  THEN m.amount ELSE 0 END), 0) AS total_debit,
		COUNT(*) AS count`).Scan(&sum).Error
	if err != nil {
		return nil, err
	}
	sum.Net = sum.TotalCredit - sum.TotalDebit

	var rows []itemRow
	err = r.base(ctx, f).Select(`m.id, m.type, m.amount, m.balance_after, m.reference_type, m.reference_id,
			m.description, m.created_at, n.id AS nasabah_id, n.full_name AS nasabah_name,
			n.class_name AS nasabah_class, n.qr_code AS nasabah_qr`).
		Order("m.created_at DESC, m.id DESC").
		Limit(page.Limit).Offset(page.Offset()).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	items := make([]Item, len(rows))
	for i, row := range rows {
		items[i] = row.toItem(withNasabah)
	}
	return &ListResponse{Items: items, Meta: utils.NewPageMeta(page, sum.Count), Summary: sum}, nil
}

// FindNasabah mengambil identitas + saldo nasabah di sekolah tertentu
func (r *Repository) FindNasabah(ctx context.Context, schoolID *string, id string) (*NasabahBalance, error) {
	var rows []struct {
		ID        string
		FullName  string
		ClassName *string
		QRCode    *string
		Status    string
		Balance   int64
	}
	err := r.db.WithContext(ctx).Raw(`
		SELECT id, full_name, class_name, qr_code, status, balance FROM users
		WHERE id = ? AND role = 'nasabah' AND school_id IS NOT DISTINCT FROM ?`, id, schoolID,
	).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, ErrNasabahNotFound
	}
	u := rows[0]
	return &NasabahBalance{
		NasabahRef: NasabahRef{ID: u.ID, FullName: u.FullName, ClassName: u.ClassName, QRCode: u.QRCode},
		Status:     u.Status, Balance: u.Balance,
	}, nil
}

// Balance menghitung ringkasan saldo seorang nasabah
func (r *Repository) Balance(ctx context.Context, nasabahID string) (*BalanceResponse, error) {
	var b BalanceResponse
	err := r.db.WithContext(ctx).Raw(`
		SELECT
			u.balance,
			COALESCE((SELECT SUM(amount) FROM balance_mutations WHERE nasabah_id = u.id AND type = 'credit'), 0) AS total_credit,
			COALESCE((SELECT SUM(amount) FROM balance_mutations WHERE nasabah_id = u.id AND type = 'debit'), 0)  AS total_debit,
			(SELECT COUNT(*) FROM weighings WHERE nasabah_id = u.id)                                             AS total_weighings,
			COALESCE((SELECT SUM(total_weight) FROM weighings WHERE nasabah_id = u.id), 0)                        AS total_weight_kg,
			COALESCE((SELECT SUM(amount) FROM withdrawal_requests
			          WHERE nasabah_id = u.id AND status IN ('pending', 'approved')), 0)                         AS pending_withdrawal_amount,
			(SELECT MAX(created_at) FROM balance_mutations WHERE nasabah_id = u.id)                              AS last_mutation_at
		FROM users u WHERE u.id = ?`, nasabahID,
	).Scan(&b).Error
	if err != nil {
		return nil, err
	}
	b.AvailableBalance = b.Balance - b.PendingWithdrawalAmount
	return &b, nil
}
