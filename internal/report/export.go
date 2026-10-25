package report

import (
	"context"
	"database/sql"
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/banksampahku/backend/internal/auth"
)

// utf8BOM membuat Excel membaca file CSV sebagai UTF-8 (huruf & simbol tidak rusak)
const utf8BOM = "\xEF\xBB\xBF"

var (
	exportMutationTypes = map[string]string{"credit": "Saldo Masuk", "debit": "Saldo Keluar"}
	exportSources       = map[string]string{
		"weighing": "Setoran Sampah", "withdrawal": "Penarikan Saldo", "adjustment": "Koreksi Saldo",
	}
)

// ExportJob adalah ekspor yang siap ditulis: nama file + fungsi penulis.
// Dipisah agar handler bisa memasang header HTTP sebelum data dialirkan.
type ExportJob struct {
	Filename string
	Write    func(w io.Writer) error
}

// csvFormat mengatur pemisah kolom & desimal
type csvFormat struct {
	comma        rune
	decimalComma bool // true: 2,50 (Excel Indonesia); false: 2.50
}

func (f csvFormat) decimal(s string) string {
	if f.decimalComma {
		return strings.Replace(s, ".", ",", 1)
	}
	return s
}

// safeText mencegah CSV/Formula injection: teks yang diawali = + - @ akan dijalankan
// Excel sebagai rumus, jadi diberi awalan tanda kutip tunggal.
func safeText(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}

func nullText(ns sql.NullString) string {
	if !ns.Valid {
		return ""
	}
	return safeText(ns.String)
}

// Export menyiapkan ekspor CSV sesuai jenis & periode
func (s *Service) Export(ctx context.Context, admin *auth.User, q ExportQuery) (*ExportJob, error) {
	p, _, err := resolvePeriod(q.PeriodQuery)
	if err != nil {
		return nil, err
	}

	typ := q.Type
	if typ == "" {
		typ = "by-nasabah"
	}
	f := csvFormat{comma: ';', decimalComma: true}
	if q.Delimiter == "comma" {
		f = csvFormat{comma: ',', decimalComma: false}
	}

	job := &ExportJob{Filename: fmt.Sprintf("laporan-%s_%s_sd_%s.csv", typ, p.From, p.To)}
	job.Write = func(w io.Writer) error {
		if _, err := io.WriteString(w, utf8BOM); err != nil {
			return err
		}
		cw := csv.NewWriter(w)
		cw.Comma = f.comma

		var err error
		switch typ {
		case "by-category":
			err = s.writeByCategory(ctx, cw, f, admin.SchoolID, p)
		case "weighings":
			err = s.writeWeighings(ctx, cw, f, admin.SchoolID, p)
		case "mutations":
			err = s.writeMutations(ctx, cw, admin.SchoolID, p)
		default:
			err = s.writeByNasabah(ctx, cw, f, admin.SchoolID, p)
		}
		if err != nil {
			return err
		}
		cw.Flush()
		return cw.Error()
	}
	return job, nil
}

// streamRows membaca *sql.Rows baris demi baris; flush berkala agar data langsung terkirim
func streamRows(rows *sql.Rows, cw *csv.Writer, scan func() ([]string, error)) error {
	defer rows.Close()
	n := 0
	for rows.Next() {
		record, err := scan()
		if err != nil {
			return err
		}
		if err := cw.Write(record); err != nil {
			return err
		}
		if n++; n%500 == 0 {
			cw.Flush()
		}
	}
	return rows.Err()
}

func (s *Service) writeByNasabah(ctx context.Context, cw *csv.Writer, f csvFormat, schoolID *string, p period) error {
	_ = cw.Write([]string{"Nama Nasabah", "Kelas", "QR Code", "Jumlah Setor", "Total Berat (kg)",
		"Total Setoran (Rp)", "Total Ditarik (Rp)", "Saldo Saat Ini (Rp)"})
	rows, err := s.repo.exportNasabahRows(ctx, schoolID, p)
	if err != nil {
		return err
	}
	return streamRows(rows, cw, func() ([]string, error) {
		var name, weight string
		var class, qr sql.NullString
		var count, amount, withdrawn, balance int64
		if err := rows.Scan(&name, &class, &qr, &count, &weight, &amount, &withdrawn, &balance); err != nil {
			return nil, err
		}
		return []string{safeText(name), nullText(class), nullText(qr), itoa(count), f.decimal(weight),
			itoa(amount), itoa(withdrawn), itoa(balance)}, nil
	})
}

func (s *Service) writeByCategory(ctx context.Context, cw *csv.Writer, f csvFormat, schoolID *string, p period) error {
	_ = cw.Write([]string{"Kategori", "Jenis Sampah", "Satuan", "Jumlah", "Total Nilai (Rp)", "Jumlah Transaksi"})
	rows, err := s.repo.exportTypeRows(ctx, schoolID, p)
	if err != nil {
		return err
	}
	return streamRows(rows, cw, func() ([]string, error) {
		var cat, typ, unit, qty string
		var amount, count int64
		if err := rows.Scan(&cat, &typ, &unit, &qty, &amount, &count); err != nil {
			return nil, err
		}
		return []string{safeText(cat), safeText(typ), unit, f.decimal(qty), itoa(amount), itoa(count)}, nil
	})
}

func (s *Service) writeWeighings(ctx context.Context, cw *csv.Writer, f csvFormat, schoolID *string, p period) error {
	_ = cw.Write([]string{"Tanggal", "ID Transaksi", "Nama Nasabah", "Kelas", "QR Code", "Kategori",
		"Jenis Sampah", "Berat", "Satuan", "Harga per Satuan (Rp)", "Subtotal (Rp)", "Petugas"})
	rows, err := s.repo.exportWeighingRows(ctx, schoolID, p)
	if err != nil {
		return err
	}
	return streamRows(rows, cw, func() ([]string, error) {
		var date, id, name, cat, typ, weight, unit, admin string
		var class, qr sql.NullString
		var price, subtotal int64
		if err := rows.Scan(&date, &id, &name, &class, &qr, &cat, &typ, &weight, &unit, &price, &subtotal, &admin); err != nil {
			return nil, err
		}
		return []string{date, id, safeText(name), nullText(class), nullText(qr), safeText(cat), safeText(typ),
			f.decimal(weight), unit, itoa(price), itoa(subtotal), safeText(admin)}, nil
	})
}

func (s *Service) writeMutations(ctx context.Context, cw *csv.Writer, schoolID *string, p period) error {
	_ = cw.Write([]string{"Waktu", "Nama Nasabah", "QR Code", "Jenis", "Sumber", "Nominal (Rp)",
		"Saldo Setelah (Rp)", "Keterangan"})
	rows, err := s.repo.exportMutationRows(ctx, schoolID, p)
	if err != nil {
		return err
	}
	return streamRows(rows, cw, func() ([]string, error) {
		var at time.Time
		var name, typ, ref string
		var qr, desc sql.NullString
		var amount, after int64
		if err := rows.Scan(&at, &name, &qr, &typ, &ref, &amount, &after, &desc); err != nil {
			return nil, err
		}
		return []string{at.In(time.Local).Format("2006-01-02 15:04"), safeText(name), nullText(qr),
			exportMutationTypes[typ], exportSources[ref], itoa(amount), itoa(after), nullText(desc)}, nil
	})
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
