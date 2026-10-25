package report

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/banksampahku/backend/internal/utils"
)

// Repository berisi query agregasi laporan. Semua query dibatasi ke sekolah admin.
// Periode tanggal transaksi (DATE) dibandingkan langsung; kolom waktu (TIMESTAMPTZ)
// memakai rentang [fromTime, toTime) menurut zona waktu aplikasi.
type Repository struct {
	db *gorm.DB
}

// NewRepository membuat repository laporan
func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

// period adalah periode yang sudah dinormalisasi
type period struct {
	From, To         string    // YYYY-MM-DD (inklusif) untuk kolom DATE
	FromTime, ToTime time.Time // [from 00:00, to+1 00:00) untuk kolom TIMESTAMPTZ
}

// ---------- Summary ----------

// WeighingStats menghitung statistik penimbangan pada periode
func (r *Repository) WeighingStats(ctx context.Context, schoolID *string, p period) (WeighingStats, error) {
	var s WeighingStats
	err := r.db.WithContext(ctx).Raw(`
		SELECT COUNT(*) AS count,
			COALESCE(SUM(total_weight), 0) AS total_weight_kg,
			COALESCE(SUM(total_amount), 0) AS total_amount,
			COUNT(DISTINCT nasabah_id) AS active_nasabah
		FROM weighings
		WHERE school_id IS NOT DISTINCT FROM ? AND transaction_date BETWEEN ?::date AND ?::date`,
		schoolID, p.From, p.To,
	).Scan(&s).Error
	if s.Count > 0 {
		s.AverageAmount = s.TotalAmount / s.Count
	}
	return s, err
}

// MutationStats menghitung arus saldo masuk/keluar pada periode
func (r *Repository) MutationStats(ctx context.Context, schoolID *string, p period) (MutationStats, error) {
	var s MutationStats
	err := r.db.WithContext(ctx).Raw(`
		SELECT COALESCE(SUM(CASE WHEN m.type = 'credit' THEN m.amount END), 0) AS total_credit,
			COALESCE(SUM(CASE WHEN m.type = 'debit' THEN m.amount END), 0) AS total_debit
		FROM balance_mutations m JOIN users n ON n.id = m.nasabah_id
		WHERE n.school_id IS NOT DISTINCT FROM ? AND m.created_at >= ? AND m.created_at < ?`,
		schoolID, p.FromTime, p.ToTime,
	).Scan(&s).Error
	s.Net = s.TotalCredit - s.TotalDebit
	return s, err
}

// WithdrawalStats menghitung penarikan selesai (periode) & yang sedang diproses (saat ini)
func (r *Repository) WithdrawalStats(ctx context.Context, schoolID *string, p period) (WithdrawalStats, error) {
	var s WithdrawalStats
	err := r.db.WithContext(ctx).Raw(`
		SELECT
			COUNT(*) FILTER (WHERE w.status = 'completed' AND w.verified_at >= ? AND w.verified_at < ?) AS completed_count,
			COALESCE(SUM(w.amount) FILTER (WHERE w.status = 'completed' AND w.verified_at >= ? AND w.verified_at < ?), 0) AS completed_amount,
			COUNT(*) FILTER (WHERE w.status IN ('pending', 'approved')) AS in_process_count,
			COALESCE(SUM(w.amount) FILTER (WHERE w.status IN ('pending', 'approved')), 0) AS in_process_amount
		FROM withdrawal_requests w JOIN users n ON n.id = w.nasabah_id
		WHERE n.school_id IS NOT DISTINCT FROM ?`,
		p.FromTime, p.ToTime, p.FromTime, p.ToTime, schoolID,
	).Scan(&s).Error
	return s, err
}

// Snapshot menghitung kondisi nasabah & total saldo beredar saat ini
func (r *Repository) Snapshot(ctx context.Context, schoolID *string) (Snapshot, error) {
	var s Snapshot
	err := r.db.WithContext(ctx).Raw(`
		SELECT COUNT(*) FILTER (WHERE status = 'active') AS active_nasabah,
			COUNT(*) FILTER (WHERE status = 'pending') AS pending_nasabah,
			COALESCE(SUM(balance), 0) AS outstanding_saldo
		FROM users WHERE role = 'nasabah' AND school_id IS NOT DISTINCT FROM ?`, schoolID,
	).Scan(&s).Error
	return s, err
}

// Daily menghitung tren harian; hari tanpa transaksi tetap muncul bernilai 0
func (r *Repository) Daily(ctx context.Context, schoolID *string, p period) ([]DailyPoint, error) {
	var rows []struct {
		Day           time.Time
		Count         int64
		TotalWeightKg float64
		TotalAmount   int64
	}
	err := r.db.WithContext(ctx).Raw(`
		SELECT d::date AS day, COUNT(w.id) AS count,
			COALESCE(SUM(w.total_weight), 0) AS total_weight_kg,
			COALESCE(SUM(w.total_amount), 0) AS total_amount
		FROM generate_series(?::date, ?::date, INTERVAL '1 day') AS d
		LEFT JOIN weighings w ON w.transaction_date = d::date AND w.school_id IS NOT DISTINCT FROM ?
		GROUP BY d ORDER BY d`,
		p.From, p.To, schoolID,
	).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]DailyPoint, len(rows))
	for i, rw := range rows {
		out[i] = DailyPoint{Date: rw.Day.Format("2006-01-02"), Count: rw.Count,
			TotalWeightKg: rw.TotalWeightKg, TotalAmount: rw.TotalAmount}
	}
	return out, nil
}

// ---------- Per kategori ----------

// typeAgg adalah rekap satu jenis sampah (bahan laporan per kategori)
type typeAgg struct {
	CategoryID    string
	CategoryName  string
	TypeID        string
	TypeName      string
	Unit          string
	Quantity      float64
	TotalAmount   int64
	WeighingCount int64
}

// ByType merekap item penimbangan per jenis sampah pada periode
func (r *Repository) ByType(ctx context.Context, schoolID *string, p period) ([]typeAgg, error) {
	var rows []typeAgg
	err := r.db.WithContext(ctx).Raw(`
		SELECT c.id AS category_id, c.name AS category_name, t.id AS type_id, t.name AS type_name, t.unit,
			SUM(wi.weight) AS quantity, SUM(wi.subtotal) AS total_amount,
			COUNT(DISTINCT w.id) AS weighing_count
		FROM weighing_items wi
		JOIN weighings w ON w.id = wi.weighing_id
		JOIN waste_types t ON t.id = wi.waste_type_id
		JOIN waste_categories c ON c.id = t.category_id
		WHERE w.school_id IS NOT DISTINCT FROM ? AND w.transaction_date BETWEEN ?::date AND ?::date
		GROUP BY c.id, c.name, t.id, t.name, t.unit
		ORDER BY c.name, total_amount DESC`,
		schoolID, p.From, p.To,
	).Scan(&rows).Error
	return rows, err
}

// CategoryWeighingCounts menghitung jumlah transaksi unik per kategori
// (satu penimbangan bisa berisi beberapa jenis dari kategori yang sama)
func (r *Repository) CategoryWeighingCounts(ctx context.Context, schoolID *string, p period) (map[string]int64, error) {
	var rows []struct {
		CategoryID string
		N          int64
	}
	err := r.db.WithContext(ctx).Raw(`
		SELECT t.category_id, COUNT(DISTINCT w.id) AS n
		FROM weighing_items wi
		JOIN weighings w ON w.id = wi.weighing_id
		JOIN waste_types t ON t.id = wi.waste_type_id
		WHERE w.school_id IS NOT DISTINCT FROM ? AND w.transaction_date BETWEEN ?::date AND ?::date
		GROUP BY t.category_id`,
		schoolID, p.From, p.To,
	).Scan(&rows).Error
	out := make(map[string]int64, len(rows))
	for _, rw := range rows {
		out[rw.CategoryID] = rw.N
	}
	return out, err
}

// ---------- Per nasabah ----------

var nasabahSorts = map[string]string{
	"":               "total_amount DESC, full_name",
	"amount_desc":    "total_amount DESC, full_name",
	"weight_desc":    "total_weight_kg DESC, full_name",
	"withdrawn_desc": "total_withdrawn DESC, full_name",
	"balance_desc":   "balance DESC, full_name",
	"name":           "full_name",
}

// byNasabahSQL menyusun query rekap per nasabah (dipakai daftar & ekspor)
func byNasabahSQL(schoolID *string, p period, search string, all bool) (string, []any) {
	q := `
		SELECT n.id, n.full_name, n.class_name, n.qr_code, n.status, n.balance,
			COALESCE(w.cnt, 0) AS weighing_count,
			COALESCE(w.kg, 0) AS total_weight_kg,
			COALESCE(w.amt, 0) AS total_amount,
			COALESCE(d.withdrawn, 0) AS total_withdrawn
		FROM users n
		LEFT JOIN LATERAL (
			SELECT COUNT(*) AS cnt, SUM(total_weight) AS kg, SUM(total_amount) AS amt
			FROM weighings WHERE nasabah_id = n.id AND transaction_date BETWEEN ?::date AND ?::date
		) w ON TRUE
		LEFT JOIN LATERAL (
			SELECT SUM(amount) AS withdrawn FROM balance_mutations
			WHERE nasabah_id = n.id AND type = 'debit' AND reference_type = 'withdrawal'
			  AND created_at >= ? AND created_at < ?
		) d ON TRUE
		WHERE n.role = 'nasabah' AND n.school_id IS NOT DISTINCT FROM ?`
	args := []any{p.From, p.To, p.FromTime, p.ToTime, schoolID}

	if s := strings.TrimSpace(search); s != "" {
		like := "%" + utils.EscapeLike(s) + "%"
		q += ` AND (n.full_name ILIKE ? OR n.qr_code ILIKE ? OR n.class_name ILIKE ?)`
		args = append(args, like, like, like)
	}
	if !all {
		q += ` AND (COALESCE(w.cnt, 0) > 0 OR COALESCE(d.withdrawn, 0) > 0)`
	}
	return q, args
}

// ByNasabah mengambil rekap per nasabah dengan urutan & pagination
func (r *Repository) ByNasabah(ctx context.Context, schoolID *string, p period, q ByNasabahQuery) ([]NasabahRow, int64, error) {
	base, args := byNasabahSQL(schoolID, p, q.Search, q.All)

	var total int64
	if err := r.db.WithContext(ctx).Raw(`SELECT COUNT(*) FROM (`+base+`) x`, args...).Scan(&total).Error; err != nil {
		return nil, 0, err
	}

	var rows []NasabahRow
	err := r.db.WithContext(ctx).Raw(
		`SELECT * FROM (`+base+`) x ORDER BY `+nasabahSorts[q.Sort]+` LIMIT ? OFFSET ?`,
		append(args, q.Limit, q.Offset())...,
	).Scan(&rows).Error
	return rows, total, err
}

// ---------- Ekspor (streaming baris demi baris) ----------

// Rows menjalankan query mentah dan mengembalikan *sql.Rows untuk dibaca bertahap
func (r *Repository) Rows(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return r.db.WithContext(ctx).Raw(query, args...).Rows()
}

// exportNasabahRows mengalirkan rekap per nasabah (tanpa pagination)
func (r *Repository) exportNasabahRows(ctx context.Context, schoolID *string, p period) (*sql.Rows, error) {
	base, args := byNasabahSQL(schoolID, p, "", false)
	return r.Rows(ctx, `SELECT full_name, class_name, qr_code, weighing_count, total_weight_kg::text,
		total_amount, total_withdrawn, balance FROM (`+base+`) x ORDER BY total_amount DESC, full_name`, args...)
}

// exportTypeRows mengalirkan rekap per jenis sampah
func (r *Repository) exportTypeRows(ctx context.Context, schoolID *string, p period) (*sql.Rows, error) {
	return r.Rows(ctx, `
		SELECT c.name, t.name, t.unit, SUM(wi.weight)::text, SUM(wi.subtotal), COUNT(DISTINCT w.id)
		FROM weighing_items wi
		JOIN weighings w ON w.id = wi.weighing_id
		JOIN waste_types t ON t.id = wi.waste_type_id
		JOIN waste_categories c ON c.id = t.category_id
		WHERE w.school_id IS NOT DISTINCT FROM ? AND w.transaction_date BETWEEN ?::date AND ?::date
		GROUP BY c.name, t.name, t.unit
		ORDER BY c.name, SUM(wi.subtotal) DESC`, schoolID, p.From, p.To)
}

// exportWeighingRows mengalirkan detail penimbangan (satu baris per jenis sampah)
func (r *Repository) exportWeighingRows(ctx context.Context, schoolID *string, p period) (*sql.Rows, error) {
	return r.Rows(ctx, `
		SELECT to_char(w.transaction_date, 'YYYY-MM-DD'), w.id, n.full_name, n.class_name, n.qr_code,
			c.name, t.name, wi.weight::text, t.unit, wi.price_per_unit, wi.subtotal, a.full_name
		FROM weighing_items wi
		JOIN weighings w ON w.id = wi.weighing_id
		JOIN users n ON n.id = w.nasabah_id
		JOIN users a ON a.id = w.admin_id
		JOIN waste_types t ON t.id = wi.waste_type_id
		JOIN waste_categories c ON c.id = t.category_id
		WHERE w.school_id IS NOT DISTINCT FROM ? AND w.transaction_date BETWEEN ?::date AND ?::date
		ORDER BY w.transaction_date, w.created_at, c.name, t.name`, schoolID, p.From, p.To)
}

// exportMutationRows mengalirkan mutasi saldo
func (r *Repository) exportMutationRows(ctx context.Context, schoolID *string, p period) (*sql.Rows, error) {
	return r.Rows(ctx, `
		SELECT m.created_at, n.full_name, n.qr_code, m.type, m.reference_type, m.amount, m.balance_after, m.description
		FROM balance_mutations m JOIN users n ON n.id = m.nasabah_id
		WHERE n.school_id IS NOT DISTINCT FROM ? AND m.created_at >= ? AND m.created_at < ?
		ORDER BY m.created_at, m.id`, schoolID, p.FromTime, p.ToTime)
}
