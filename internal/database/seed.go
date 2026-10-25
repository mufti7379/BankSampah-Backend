package database

import (
	"fmt"
	"time"

	"github.com/rs/zerolog/log"
	"gorm.io/gorm"

	"github.com/banksampahku/backend/internal/utils"
)

// Kredensial akun contoh hasil seeder (HANYA untuk development)
const (
	seedAdminEmail      = "admin@banksampahku.id"
	seedAdminPassword   = "Admin#2025"
	seedNasabahPassword = "Nasabah#2025"
)

type seedNasabah struct {
	FullName  string
	Email     string
	Phone     string
	ClassName string
	QRCode    string
}

type seedWasteType struct {
	Name         string
	PricePerUnit int64 // Rupiah per kg
}

type seedCategory struct {
	Name        string
	Description string
	Types       []seedWasteType
}

// Data dummy sesuai PRD Bab 9
var (
	seedNasabahList = []seedNasabah{
		{FullName: "Aisyah Ramadhani", Email: "aisyah@banksampahku.id", Phone: "081234567801", ClassName: "8A", QRCode: "BSK-7HQ2D9LM"},
		{FullName: "Bagas Prakoso", Email: "bagas@banksampahku.id", Phone: "081234567802", ClassName: "7C", QRCode: "BSK-3WY8P1KQ"},
	}

	seedCategoryList = []seedCategory{
		{
			Name:        "Plastik",
			Description: "Sampah berbahan plastik seperti botol, gelas, dan tutup botol",
			Types: []seedWasteType{
				{Name: "Botol PET", PricePerUnit: 3500},
				{Name: "Gelas Plastik", PricePerUnit: 2800},
				{Name: "Tutup Botol", PricePerUnit: 4200},
			},
		},
		{
			Name:        "Kertas",
			Description: "Sampah kertas dan karton dalam kondisi kering",
			Types: []seedWasteType{
				{Name: "Kardus", PricePerUnit: 2000},
				{Name: "HVS Putih", PricePerUnit: 2500},
				{Name: "Koran", PricePerUnit: 1800},
			},
		},
		{
			Name:        "Logam",
			Description: "Sampah logam seperti kaleng, besi, dan tembaga",
			Types: []seedWasteType{
				{Name: "Kaleng Aluminium", PricePerUnit: 12000},
				{Name: "Besi Tua", PricePerUnit: 4500},
				{Name: "Tembaga", PricePerUnit: 65000},
			},
		},
		{
			Name:        "Kaca",
			Description: "Sampah kaca berupa botol dan pecahan kaca",
			Types: []seedWasteType{
				{Name: "Botol Kaca Bening", PricePerUnit: 500},
				{Name: "Botol Kaca Warna", PricePerUnit: 400},
				{Name: "Pecahan Kaca", PricePerUnit: 200},
			},
		},
	}
)

// Seed mengisi data awal (sekolah, admin, nasabah, kategori, jenis sampah, harga).
// Seluruh proses berjalan dalam satu DB transaction. Jika akun admin contoh
// sudah ada, seeder dilewati agar aman dijalankan berulang kali.
func Seed(db *gorm.DB, hasher *utils.PasswordHasher) error {
	var existing int64
	if err := db.Raw("SELECT COUNT(*) FROM users WHERE email = ?", seedAdminEmail).Scan(&existing).Error; err != nil {
		return fmt.Errorf("gagal memeriksa data seed: %w", err)
	}
	if existing > 0 {
		log.Info().Str("email", seedAdminEmail).Msg("Data seed sudah ada, seeder dilewati")
		return nil
	}

	// Hash password (bcrypt + pepper) di luar transaksi karena bcrypt cost 12 relatif lambat.
	// bcrypt membangkitkan salt acak 16 byte di setiap pemanggilan, sehingga
	// setiap user di-hash terpisah agar password sama tetap menghasilkan hash berbeda.
	adminHash, err := hasher.Hash(seedAdminPassword)
	if err != nil {
		return fmt.Errorf("gagal hash password admin: %w", err)
	}
	nasabahHashes := make([]string, len(seedNasabahList))
	for i, n := range seedNasabahList {
		nasabahHashes[i], err = hasher.Hash(seedNasabahPassword)
		if err != nil {
			return fmt.Errorf("gagal hash password nasabah %s: %w", n.FullName, err)
		}
	}

	effectiveDate := time.Now().Format("2006-01-02")

	return db.Transaction(func(tx *gorm.DB) error {
		// 1) Sekolah contoh
		var schoolID string
		if err := tx.Raw(`
			INSERT INTO schools (name, address, phone, headmaster)
			VALUES (?, ?, ?, ?)
			RETURNING id`,
			"SMP Harapan Hijau",
			"Jl. Lingkungan Lestari No. 12, Sleman, Yogyakarta",
			"0274123456",
			"Drs. Hendra Gunawan",
		).Scan(&schoolID).Error; err != nil {
			return fmt.Errorf("gagal membuat sekolah: %w", err)
		}

		// 2) Akun admin
		var adminID string
		if err := tx.Raw(`
			INSERT INTO users (school_id, email, password_hash, full_name, phone, role, status)
			VALUES (?, ?, ?, ?, ?, 'admin', 'active')
			RETURNING id`,
			schoolID, seedAdminEmail, adminHash, "Sari Wulandari", "081234567800",
		).Scan(&adminID).Error; err != nil {
			return fmt.Errorf("gagal membuat admin: %w", err)
		}

		// 3) Nasabah contoh (saldo awal 0 agar konsisten dengan ledger mutasi)
		for i, n := range seedNasabahList {
			if err := tx.Exec(`
				INSERT INTO users (school_id, email, password_hash, full_name, phone, class_name, role, status, qr_code)
				VALUES (?, ?, ?, ?, ?, ?, 'nasabah', 'active', ?)`,
				schoolID, n.Email, nasabahHashes[i], n.FullName, n.Phone, n.ClassName, n.QRCode,
			).Error; err != nil {
				return fmt.Errorf("gagal membuat nasabah %s: %w", n.FullName, err)
			}
		}

		// 4) Kategori, jenis sampah, dan harga awal berlaku hari ini
		typeCount := 0
		for _, c := range seedCategoryList {
			var categoryID string
			if err := tx.Raw(`
				INSERT INTO waste_categories (school_id, name, description)
				VALUES (?, ?, ?)
				RETURNING id`,
				schoolID, c.Name, c.Description,
			).Scan(&categoryID).Error; err != nil {
				return fmt.Errorf("gagal membuat kategori %s: %w", c.Name, err)
			}

			for _, t := range c.Types {
				var typeID string
				if err := tx.Raw(`
					INSERT INTO waste_types (category_id, name, unit)
					VALUES (?, ?, 'kg')
					RETURNING id`,
					categoryID, t.Name,
				).Scan(&typeID).Error; err != nil {
					return fmt.Errorf("gagal membuat jenis sampah %s: %w", t.Name, err)
				}

				if err := tx.Exec(`
					INSERT INTO price_histories (waste_type_id, price_per_unit, effective_date, set_by, note)
					VALUES (?, ?, ?, ?, ?)`,
					typeID, t.PricePerUnit, effectiveDate, adminID, "Harga awal (seeder)",
				).Error; err != nil {
					return fmt.Errorf("gagal membuat harga %s: %w", t.Name, err)
				}
				typeCount++
			}
		}

		log.Info().
			Str("school_id", schoolID).
			Str("admin", seedAdminEmail).
			Int("nasabah", len(seedNasabahList)).
			Int("kategori", len(seedCategoryList)).
			Int("jenis_sampah", typeCount).
			Str("harga_berlaku", effectiveDate).
			Msg("Seeder data awal berhasil dijalankan")
		return nil
	})
}
