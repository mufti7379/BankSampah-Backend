package weighing

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	"gorm.io/gorm"

	"github.com/banksampahku/backend/internal/audit"
	"github.com/banksampahku/backend/internal/auth"
	"github.com/banksampahku/backend/internal/mutation"
	"github.com/banksampahku/backend/internal/notification"
	"github.com/banksampahku/backend/internal/utils"
	"github.com/banksampahku/backend/internal/waste"
)

// Error domain penimbangan
var (
	ErrNasabahNotFound   = errors.New("nasabah tidak ditemukan")
	ErrNasabahNotActive  = errors.New("akun nasabah tidak aktif")
	ErrNasabahIdentifier = errors.New("isi salah satu: qr_code atau nasabah_id")
	ErrInvalidDateRange  = errors.New("date_from tidak boleh setelah date_to")
	// ErrIdempotencyConflict: Idempotency-Key sudah dipakai admin ini untuk nasabah lain
	ErrIdempotencyConflict = errors.New("idempotency key sudah dipakai untuk transaksi lain")

	// errReplay menghentikan DB transaction tanpa menulis apa pun karena transaksi sudah ada
	errReplay = errors.New("penimbangan sudah tercatat dengan idempotency key yang sama")
)

// idempotencyIndex adalah unique index (admin_id, idempotency_key) di migration 0007
const idempotencyIndex = "uq_weighings_admin_idempotency"

// FieldErrors adalah kumpulan error validasi bisnis per field (dikirim sebagai 422),
// misal {"items[1].waste_type_id": "Belum ada harga yang berlaku ..."}
type FieldErrors map[string]string

func (e FieldErrors) Error() string { return "data penimbangan tidak valid" }

// Service berisi logika bisnis penimbangan
type Service struct {
	repo *Repository
}

// NewService membuat service penimbangan
func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

// today mengembalikan tanggal hari ini (zona waktu aplikasi) tanpa jam
func today() time.Time {
	t, _ := time.ParseInLocation(DateLayout, time.Now().Format(DateLayout), time.Local)
	return t
}

// toCenti mengubah berat (kg) menjadi satuan 1/100 kg dan memastikan maksimal 2 desimal.
// Perhitungan uang memakai bilangan bulat agar tidak ada selisih pembulatan float.
func toCenti(weight float64) (int64, bool) {
	scaled := weight * weightScale
	centi := int64(math.Round(scaled))
	return centi, math.Abs(scaled-float64(centi)) < 1e-6
}

// centiToString memformat 250 → "2.50" (untuk kolom NUMERIC(10,2))
func centiToString(centi int64) string {
	return fmt.Sprintf("%d.%02d", centi/weightScale, centi%weightScale)
}

// subtotal = berat × harga, dibulatkan ke rupiah terdekat (PRD Bab 6D).
// Contoh: 2.5 kg × Rp3.500 = 250 × 3500 / 100 = Rp8.750
func subtotal(centi, pricePerUnit int64) int64 {
	return (centi*pricePerUnit + weightScale/2) / weightScale
}

// resolveDate menentukan tanggal transaksi & memvalidasi aturan back-date
func resolveDate(req CreateRequest) (string, FieldErrors) {
	if req.TransactionDate == "" {
		return today().Format(DateLayout), nil
	}
	date, err := time.ParseInLocation(DateLayout, req.TransactionDate, time.Local)
	if err != nil {
		return "", FieldErrors{"transaction_date": "Format tanggal harus YYYY-MM-DD"}
	}

	now := today()
	switch {
	case date.After(now):
		return "", FieldErrors{"transaction_date": "Tanggal transaksi tidak boleh di masa depan"}
	case date.Before(now.AddDate(0, 0, -MaxBackdateDays)):
		return "", FieldErrors{"transaction_date": fmt.Sprintf("Tanggal transaksi maksimal %d hari ke belakang", MaxBackdateDays)}
	case date.Before(now) && (req.Note == nil || strings.TrimSpace(*req.Note) == ""):
		return "", FieldErrors{"note": "Alasan wajib diisi jika tanggal transaksi bukan hari ini"}
	}
	return req.TransactionDate, nil
}

// Lookup mencari nasabah dari hasil scan QR (untuk ditampilkan sebelum penimbangan)
func (s *Service) Lookup(ctx context.Context, admin *auth.User, qrCode string) (*LookupResponse, error) {
	u, err := s.repo.FindNasabah(ctx, admin.SchoolID, "", strings.TrimSpace(qrCode))
	if err != nil {
		return nil, err
	}
	qr := ""
	if u.QRCode != nil {
		qr = *u.QRCode
	}
	return &LookupResponse{
		ID: u.ID, FullName: u.FullName, ClassName: u.ClassName, QRCode: qr,
		Status: u.Status, Balance: u.Balance, AvatarURL: u.AvatarURL,
		CanTransact: u.Status == auth.StatusActive,
	}, nil
}

// Create mencatat transaksi penimbangan sekaligus mengonversinya menjadi saldo.
// Seluruh langkah berjalan dalam SATU DB transaction (PRD Task 3.2):
//
//	(a) simpan weighings          (d) catat balance_mutations "credit" + balance_after
//	(b) simpan weighing_items     (e) kirim notifikasi in-app ke nasabah
//	    + snapshot harga aktif    (f) catat audit_logs WEIGHING_CREATE
//	(c) saldo nasabah += total
//
// Jika salah satu langkah gagal, seluruhnya di-rollback (tidak ada data setengah jadi).
//
// Jika meta.IdempotencyKey sudah pernah dipakai admin ini untuk nasabah yang sama,
// transaksi lama dikembalikan (Replayed=true) dan saldo tidak bertambah lagi.
func (s *Service) Create(ctx context.Context, admin *auth.User, req CreateRequest, meta RequestMeta) (*CreateResult, error) {
	// 1) Identitas nasabah: tepat salah satu dari qr_code / nasabah_id
	qr := strings.TrimSpace(req.QRCode)
	if (qr == "") == (req.NasabahID == "") {
		return nil, ErrNasabahIdentifier
	}
	nasabah, err := s.repo.FindNasabah(ctx, admin.SchoolID, req.NasabahID, qr)
	if err != nil {
		return nil, err
	}
	if nasabah.Status != auth.StatusActive {
		return nil, ErrNasabahNotActive
	}

	// 2) Tanggal transaksi (default hari ini, back-date maks 7 hari + alasan)
	date, ferr := resolveDate(req)
	if ferr != nil {
		return nil, ferr
	}

	// 3) Validasi format berat & duplikasi jenis sampah sebelum menyentuh database
	ferr = FieldErrors{}
	seen := map[string]int{}
	centis := make([]int64, len(req.Items))
	ids := make([]string, 0, len(req.Items))
	for i, it := range req.Items {
		centi, ok := toCenti(it.Weight)
		if !ok {
			ferr[fmt.Sprintf("items[%d].weight", i)] = "Berat maksimal 2 angka di belakang koma"
		}
		centis[i] = centi
		if first, dup := seen[it.WasteTypeID]; dup {
			ferr[fmt.Sprintf("items[%d].waste_type_id", i)] =
				fmt.Sprintf("Jenis sampah ini sudah ada di baris %d, gabungkan beratnya", first+1)
			continue
		}
		seen[it.WasteTypeID] = i
		ids = append(ids, it.WasteTypeID)
	}
	if len(ferr) > 0 {
		return nil, ferr
	}

	note := trimPtr(req.Note)
	var weighingID string
	var ledger *mutation.Entry

	err = s.repo.DB(ctx).Transaction(func(tx *gorm.DB) error {
		// Kunci baris nasabah sampai transaksi selesai: penimbangan bersamaan untuk
		// nasabah yang sama berjalan bergantian, dan status dicek ulang secara aman.
		status, err := lockNasabahTx(tx, nasabah.ID)
		if err != nil {
			return err
		}
		if status != auth.StatusActive {
			return ErrNasabahNotActive
		}

		// Dicek SETELAH baris nasabah dikunci: kiriman ulang yang berjalan bersamaan
		// menunggu transaksi pertama commit, lalu melihat hasilnya di sini.
		if meta.IdempotencyKey != "" {
			prev, err := findByIdempotencyKeyTx(tx, admin.ID, meta.IdempotencyKey)
			if err != nil {
				return err
			}
			if prev != nil {
				if prev.NasabahID != nasabah.ID {
					return ErrIdempotencyConflict
				}
				weighingID = prev.ID
				return errReplay
			}
		}

		types, err := loadWasteTypes(tx, admin.SchoolID, ids)
		if err != nil {
			return err
		}

		// 4) Validasi jenis sampah + ambil SNAPSHOT harga aktif pada tanggal transaksi
		ferr := FieldErrors{}
		items := make([]item, 0, len(req.Items))
		names := make([]string, 0, len(req.Items))
		auditItems := make([]map[string]any, 0, len(req.Items))
		var totalAmount, totalCenti int64
		for i, it := range req.Items {
			key := fmt.Sprintf("items[%d].waste_type_id", i)
			wt, ok := types[it.WasteTypeID]
			switch {
			case !ok:
				ferr[key] = "Jenis sampah tidak ditemukan"
				continue
			case !wt.IsActive || !wt.CategoryActive:
				ferr[key] = fmt.Sprintf("Jenis sampah %q sedang nonaktif", wt.Name)
				continue
			}

			price, err := waste.ActivePriceTx(tx, wt.ID, date)
			if errors.Is(err, waste.ErrNoActivePrice) {
				ferr[key] = fmt.Sprintf("Belum ada harga %q yang berlaku pada %s", wt.Name, date)
				continue
			}
			if err != nil {
				return err
			}

			sub := subtotal(centis[i], price.PricePerUnit)
			items = append(items, item{
				WasteTypeID:  wt.ID,
				Weight:       centiToString(centis[i]),
				PricePerUnit: price.PricePerUnit,
				Subtotal:     sub,
			})
			totalAmount += sub
			totalCenti += centis[i]
			names = append(names, wt.Name)
			auditItems = append(auditItems, map[string]any{
				"waste_type": wt.Name, "weight": centiToString(centis[i]),
				"price_per_unit": price.PricePerUnit, "subtotal": sub,
			})
		}
		if len(ferr) > 0 {
			return ferr // rollback: belum ada yang tersimpan
		}
		// balance_mutations.amount wajib > 0 (misal 0,01 kg × Rp1 dibulatkan menjadi Rp0)
		if totalAmount <= 0 {
			return FieldErrors{"items": "Total nilai penimbangan Rp0, periksa kembali berat sampah"}
		}

		// (a)+(b) Simpan header + item dengan snapshot harga
		weighingID, err = insertWeighing(tx, header{
			NasabahID:       nasabah.ID,
			AdminID:         admin.ID,
			SchoolID:        admin.SchoolID,
			TransactionDate: date,
			TotalAmount:     totalAmount,
			TotalWeight:     centiToString(totalCenti),
			Note:            note,
			IdempotencyKey:  trimPtr(&meta.IdempotencyKey),
		}, items)
		if err != nil {
			// Key sama untuk nasabah berbeda yang dikirim bersamaan (kunci baris berbeda)
			if utils.UniqueViolation(err) == idempotencyIndex {
				return ErrIdempotencyConflict
			}
			return err
		}

		dateTime, _ := time.ParseInLocation(DateLayout, date, time.Local)
		kg := utils.FormatDecimal2(totalCenti)

		// (c)+(d) Saldo bertambah & tercatat di ledger mutasi (credit + balance_after)
		ledger, err = mutation.CreditTx(tx, nasabah.ID, totalAmount, mutation.RefWeighing, &weighingID,
			fmt.Sprintf("Setoran sampah %s kg (%s) tanggal %s", kg, joinNames(names), utils.FormatTanggal(dateTime)))
		if err != nil {
			return err
		}

		// (e) Notifikasi in-app untuk nasabah
		if err := notification.CreateTx(tx, notification.New{
			UserID: nasabah.ID,
			Type:   notification.TypeWeighingCreated,
			Title:  "Saldo Bertambah " + utils.FormatRupiah(totalAmount),
			Body: fmt.Sprintf("Penimbangan sampah Anda seberat %s kg (%s) berhasil dicatat. Saldo bertambah %s, saldo Anda sekarang %s.",
				kg, joinNames(names), utils.FormatRupiah(totalAmount), utils.FormatRupiah(ledger.BalanceAfter)),
			ReferenceID: &weighingID,
		}); err != nil {
			return err
		}

		// (f) Audit log di dalam transaksi yang sama (ikut rollback jika gagal)
		return audit.RecordTx(tx, audit.Entry{
			ActorID:   admin.ID,
			Action:    audit.ActionWeighingCreate,
			Entity:    audit.EntityWeighing,
			EntityID:  weighingID,
			IPAddress: meta.IP,
			Metadata: map[string]any{
				"source":           meta.Source,
				"nasabah_id":       nasabah.ID,
				"nasabah_name":     nasabah.FullName,
				"transaction_date": date,
				"backdated":        date != today().Format(DateLayout),
				"note":             note,
				"total_weight":     centiToString(totalCenti),
				"total_amount":     totalAmount,
				"balance_before":   ledger.BalanceBefore,
				"balance_after":    ledger.BalanceAfter,
				"mutation_id":      ledger.ID,
				"items":            auditItems,
			},
		})
	})
	if errors.Is(err, errReplay) {
		return s.replay(ctx, admin, weighingID)
	}
	if err != nil {
		return nil, err
	}

	log.Info().Str("weighing_id", weighingID).Str("nasabah_id", nasabah.ID).Str("admin_id", admin.ID).
		Int64("amount", ledger.Amount).Int64("balance_after", ledger.BalanceAfter).
		Msg("Penimbangan berhasil dicatat & saldo nasabah bertambah")

	detail, err := s.repo.FindDetail(ctx, filter{SchoolID: admin.SchoolID}, weighingID)
	if err != nil {
		return nil, err
	}
	return &CreateResult{
		Detail: *detail,
		Balance: BalanceChange{
			Before: ledger.BalanceBefore, Credit: ledger.Amount, After: ledger.BalanceAfter, MutationID: ledger.ID,
		},
	}, nil
}

// replay mengembalikan penimbangan yang sudah tercatat untuk kiriman ulang (tanpa menulis apa pun)
func (s *Service) replay(ctx context.Context, admin *auth.User, weighingID string) (*CreateResult, error) {
	detail, err := s.repo.FindDetail(ctx, filter{SchoolID: admin.SchoolID}, weighingID)
	if err != nil {
		return nil, err
	}
	balance, err := s.repo.FindCredit(ctx, weighingID)
	if err != nil {
		return nil, err
	}
	log.Info().Str("weighing_id", weighingID).Str("admin_id", admin.ID).
		Msg("Penimbangan dikirim ulang dengan Idempotency-Key yang sama, tidak dicatat dua kali")
	return &CreateResult{Detail: *detail, Balance: *balance, Replayed: true}, nil
}

// joinNames merangkum nama jenis sampah untuk teks notifikasi/mutasi,
// misal "Botol PET, Kardus" atau "Botol PET, Kardus, dan 3 lainnya"
func joinNames(names []string) string {
	const maxShown = 3
	if len(names) <= maxShown {
		return strings.Join(names, ", ")
	}
	return fmt.Sprintf("%s, dan %d lainnya", strings.Join(names[:maxShown-1], ", "), len(names)-(maxShown-1))
}

// List mengambil daftar penimbangan di sekolah admin
func (s *Service) List(ctx context.Context, admin *auth.User, q ListQuery) ([]Summary, int64, error) {
	if q.DateFrom != "" && q.DateTo != "" && q.DateFrom > q.DateTo {
		return nil, 0, ErrInvalidDateRange
	}
	return s.repo.List(ctx, filter{
		SchoolID: admin.SchoolID, NasabahID: q.NasabahID, Search: q.Search,
		DateFrom: q.DateFrom, DateTo: q.DateTo,
	}, q.PageQuery)
}

// Get mengambil detail penimbangan di sekolah admin
func (s *Service) Get(ctx context.Context, admin *auth.User, id string) (*Detail, error) {
	return s.repo.FindDetail(ctx, filter{SchoolID: admin.SchoolID}, id)
}

// ListMine mengambil daftar penimbangan milik nasabah yang sedang login
func (s *Service) ListMine(ctx context.Context, nasabah *auth.User, q MyListQuery) ([]Summary, int64, error) {
	if q.DateFrom != "" && q.DateTo != "" && q.DateFrom > q.DateTo {
		return nil, 0, ErrInvalidDateRange
	}
	return s.repo.List(ctx, filter{
		SchoolID: nasabah.SchoolID, NasabahID: nasabah.ID, DateFrom: q.DateFrom, DateTo: q.DateTo,
	}, q.PageQuery)
}

// GetMine mengambil detail penimbangan milik nasabah yang sedang login
func (s *Service) GetMine(ctx context.Context, nasabah *auth.User, id string) (*Detail, error) {
	return s.repo.FindDetail(ctx, filter{SchoolID: nasabah.SchoolID, NasabahID: nasabah.ID}, id)
}

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
