# BankSampahKu — Backend API

Backend REST API untuk sistem bank sampah sekolah **BankSampahKu**, dibangun dengan **Go (Gin + GORM)** dan **PostgreSQL**.
API ini melayani Dashboard Admin (web React) dan Aplikasi Nasabah (Flutter).

> Panduan struktur kode untuk pemula tersedia di [`docs/Panduan-Struktur-Internal-BankSampahKu.pdf`](docs/Panduan-Struktur-Internal-BankSampahKu.pdf).

---

## Daftar Isi
1. [Persiapan](#1-persiapan-sekali-saja)
2. [Menjalankan Server](#2-menjalankan-server)
3. [Mencoba Endpoint](#3-mencoba-endpoint)
4. [Test Otomatis](#4-test-otomatis)
5. [Reset Database](#5-reset-database)
6. [Masalah yang Sering Muncul](#6-masalah-yang-sering-muncul)
7. [Auto-Restart saat Kode Berubah](#7-opsional-auto-restart-saat-kode-berubah)
8. [Referensi Perintah & Endpoint](#8-referensi)

---

## 1. Persiapan (sekali saja)

| Kebutuhan | Cek dengan |
| :--- | :--- |
| Go 1.22+ | `go version` |
| PostgreSQL 15+ dengan database `banksampahku` | `psql -U postgres -l` atau pgAdmin |
| File `.env` | salin dari `.env.example` |

```powershell
cd banksampahku-backend
Copy-Item .env.example .env   # lalu edit isinya
```

Variabel di `.env` yang **wajib** diisi dengan benar:

```env
DATABASE_URL=postgres://postgres:PASSWORD_ANDA@localhost:5432/banksampahku?sslmode=disable
JWT_SECRET=...        # minimal 32 karakter acak
PASSWORD_PEPPER=...   # minimal 32 karakter acak — JANGAN diubah setelah ada user
```

Membuat nilai acak yang aman (64 karakter hex) untuk `JWT_SECRET` / `PASSWORD_PEPPER`:

```powershell
$b = New-Object byte[] 32; [Security.Cryptography.RandomNumberGenerator]::Create().GetBytes($b)
-join ($b | ForEach-Object { $_.ToString('x2') })
```

> ⚠️ Jika `PASSWORD_PEPPER` berubah, **semua password user menjadi tidak valid**. Simpan cadangannya di tempat aman.

---

## 2. Menjalankan Server

Semua perintah dijalankan dari folder `banksampahku-backend`.

**a. Migrasi database** — setiap kali ada file baru di folder `migrations/`:

```powershell
go run ./cmd/api -migrate=up
go run ./cmd/api -migrate=version   # pastikan "dirty=false"
```

**b. Isi data contoh** — sekali, atau setelah database direset:

```powershell
go run ./cmd/api -seed
```

**c. Nyalakan server:**

```powershell
go run ./cmd/api
```

Server berjalan di `http://localhost:8080`. Biarkan terminal ini terbuka, tekan `Ctrl+C` untuk mematikan.
Setiap kali kode Go diubah, matikan lalu jalankan ulang (atau gunakan [auto-restart](#7-opsional-auto-restart-saat-kode-berubah)).

**d. Cek server hidup** (di terminal lain):

```powershell
Invoke-RestMethod http://localhost:8080/health
```

Respons sehat berisi `"status": "ok"` dan `"database": "connected"`.

---

## 3. Mencoba Endpoint

### Akun contoh (dari seeder)

| Role | Email | Password | QR Code |
| :--- | :--- | :--- | :--- |
| Admin | `admin@banksampahku.id` | `Admin#2025` | — |
| Nasabah | `aisyah@banksampahku.id` | `Nasabah#2025` | `BSK-7HQ2D9LM` |
| Nasabah | `bagas@banksampahku.id` | `Nasabah#2025` | `BSK-3WY8P1KQ` |

### Cara A — PowerShell (tanpa install apa pun)

```powershell
$base = "http://localhost:8080/api/v1"

# Login admin lalu simpan token (header X-Client-Platform: mobile agar token dikirim di body)
$login = Invoke-RestMethod -Method Post -Uri "$base/auth/login" -ContentType "application/json" `
  -Headers @{ "X-Client-Platform" = "mobile" } -Body '{"email":"admin@banksampahku.id","password":"Admin#2025"}'
$h = @{ Authorization = "Bearer $($login.data.access_token)" }

# Daftar nasabah
(Invoke-RestMethod -Uri "$base/admin/nasabah" -Headers $h).data.items |
  Format-Table full_name, qr_code, balance

# Jenis sampah beserta harga aktif hari ini
(Invoke-RestMethod -Uri "$base/admin/waste-types?limit=50" -Headers $h).data.items |
  Format-Table name, category_name, current_price

# Simulasi scan QR nasabah
(Invoke-RestMethod -Uri "$base/admin/weighings/lookup?qr_code=BSK-7HQ2D9LM" -Headers $h).data
```

Melihat pesan error dari server:

```powershell
try { Invoke-RestMethod ... } catch { $_.ErrorDetails.Message }
```

### Cara B — Thunder Client (VS Code) / Postman

1. `POST http://localhost:8080/api/v1/auth/login` dengan header `X-Client-Platform: mobile` dan body JSON:
   `{"email":"admin@banksampahku.id","password":"Admin#2025"}`
   (tanpa header itu, token hanya dikirim lewat cookie httpOnly seperti untuk web)
2. Salin nilai `data.access_token` dari respons.
3. Pada request berikutnya buka tab **Auth** → pilih **Bearer Token** → tempel token.

### Format respons

Semua endpoint memakai format yang sama:

```json
{ "success": true,  "message": "...", "data": { } }
{ "success": false, "message": "...", "errors": { "field": "pesan error" } }
```

Endpoint daftar (list) mengembalikan `data.items` dan `data.meta` (`page`, `limit`, `total`, `total_pages`).
Parameter pagination: `?page=1&limit=20` (maksimal 100).

---

## 4. Test Otomatis

```powershell
go vet ./...    # cek kesalahan umum di kode
go test ./...   # jalankan seluruh unit test
gofmt -l .      # tampilkan file yang formatnya belum rapi (kosong = rapi)
```

---

## 5. Reset Database

Mengembalikan database ke kondisi awal (data seed) — berguna jika data uji sudah berantakan:

```powershell
go run ./cmd/api -migrate=version          # lihat versi saat ini, misal version=5
go run ./cmd/api -migrate=down -steps=5    # -steps = angka versi tersebut
go run ./cmd/api -migrate=up
go run ./cmd/api -seed
```

> ⚠️ Perintah ini **menghapus seluruh data**. Hanya untuk development.

---

## 6. Masalah yang Sering Muncul

| Pesan error | Penyebab & solusi |
| :--- | :--- |
| `password authentication failed` | Password di `DATABASE_URL` salah. |
| `DATABASE_URL wajib diisi` | File `.env` tidak terbaca — pastikan perintah dijalankan dari folder `banksampahku-backend`. |
| `bind: Only one usage of each socket address` | Port 8080 masih dipakai server lama. Cek: `Get-NetTCPConnection -LocalPort 8080`, lalu matikan prosesnya. |
| `Dirty database version X` | Migrasi gagal di tengah jalan. Perbaiki file SQL-nya, lalu `go run ./cmd/api -migrate=force -version=<X-1>` dan ulangi `-migrate=up`. |
| `429 Terlalu banyak permintaan` | Rate limit (120 req/menit, `/auth` 20 req/menit). Tunggu sebentar atau restart server. |
| `Akun terkunci` | Salah password 5×. Tunggu 15 menit, gunakan menu lupa password, atau restart server (development). |
| Login gagal padahal password benar | `PASSWORD_PEPPER` berubah setelah seed → reset database (bagian 5). |
| Email reset password tidak masuk | `SMTP_HOST` kosong = mode development: isi email dicetak di log server, bukan dikirim. |

---

## 7. Opsional: Auto-Restart saat Kode Berubah

Gunakan [air](https://github.com/air-verse/air) agar server otomatis restart setiap file `.go` disimpan:

```powershell
go install github.com/air-verse/air@latest
air --build.cmd "go build -o tmp\api.exe ./cmd/api" --build.bin "tmp\api.exe"
```

Folder `tmp/` sudah masuk `.gitignore`.

---

## 8. Referensi

### Perintah CLI

| Perintah | Fungsi |
| :--- | :--- |
| `go run ./cmd/api` | Menjalankan server |
| `go run ./cmd/api -migrate=up` | Menjalankan semua migrasi yang belum diterapkan |
| `go run ./cmd/api -migrate=down -steps=N` | Membatalkan N migrasi terakhir |
| `go run ./cmd/api -migrate=version` | Menampilkan versi migrasi |
| `go run ./cmd/api -migrate=force -version=N` | Memperbaiki status migrasi "dirty" |
| `go run ./cmd/api -seed` | Mengisi data contoh (ditolak jika `APP_ENV=production`) |
| `go build -o banksampahku-api.exe ./cmd/api` | Build binary |

### Endpoint yang sudah tersedia

Prefix: `/api/v1`. 🔓 = publik, 👤 = login, 🛡️ = admin, 🎒 = nasabah.

| Method | Endpoint | Akses | Keterangan |
| :--- | :--- | :---: | :--- |
| GET | `/health` *(tanpa prefix)* | 🔓 | Status server & database |
| POST | `/auth/register` | 🔓 | Daftar nasabah (status `pending`) |
| POST | `/auth/login` | 🔓 | Login → JWT (body + cookie `httpOnly`) |
| POST | `/auth/logout` | 👤 | Logout & cabut token |
| GET | `/auth/me` | 👤 | Profil user yang login |
| POST | `/auth/forgot-password` | 🔓 | Kirim link reset password ke email |
| GET | `/auth/reset-password/verify?token=` | 🔓 | Cek validitas token reset |
| POST | `/auth/reset-password` | 🔓 | Ganti password dengan token reset |
| GET | `/waste-categories` | 🔓 | Kategori aktif (cache 60 detik) |
| GET | `/notifications` | 👤 | Notifikasi milik sendiri + `unread_count` (`?status=unread`) |
| GET | `/notifications/unread-count` | 👤 | Jumlah belum dibaca (badge lonceng) |
| PATCH | `/notifications/:id/read` | 👤 | Tandai satu notifikasi dibaca |
| PATCH | `/notifications/read-all` | 👤 | Tandai semua dibaca |
| POST | `/admin/nasabah` | 🛡️ | Tambah nasabah + QR otomatis |
| GET | `/admin/nasabah` | 🛡️ | Daftar nasabah (search, status, kelas, sort) |
| GET | `/admin/nasabah/:id` | 🛡️ | Detail + statistik nasabah |
| GET | `/admin/nasabah/:id/qr.png` | 🛡️ | Gambar QR (`?size=`, `?download=true`) |
| PATCH | `/admin/nasabah/:id` | 🛡️ | Ubah data nasabah |
| PATCH | `/admin/nasabah/:id/deactivate` | 🛡️ | Nonaktifkan nasabah |
| PATCH | `/admin/nasabah/:id/activate` | 🛡️ | Setujui pendaftaran / aktifkan kembali |
| POST | `/admin/waste-categories` | 🛡️ | Tambah kategori sampah |
| GET | `/admin/waste-categories` | 🛡️ | Daftar kategori |
| PATCH | `/admin/waste-categories/:id` | 🛡️ | Ubah / aktifkan kembali kategori |
| DELETE | `/admin/waste-categories/:id` | 🛡️ | Nonaktifkan kategori (soft delete) |
| POST | `/admin/waste-types` | 🛡️ | Tambah jenis sampah |
| GET | `/admin/waste-types` | 🛡️ | Daftar jenis + harga aktif (`?category_id=`) |
| GET | `/admin/waste-types/:id` | 🛡️ | Detail jenis sampah |
| PATCH | `/admin/waste-types/:id` | 🛡️ | Ubah / pindah kategori / aktifkan kembali |
| DELETE | `/admin/waste-types/:id` | 🛡️ | Nonaktifkan jenis (soft delete) |
| POST | `/admin/waste-types/:id/prices` | 🛡️ | Set harga + tanggal berlaku |
| GET | `/admin/waste-types/:id/prices` | 🛡️ | Histori harga (active/scheduled/expired) |
| GET | `/admin/waste-types/:id/price-active?date=` | 🛡️ | Harga aktif pada tanggal tertentu |
| DELETE | `/admin/waste-types/:id/prices/:priceId` | 🛡️ | Batalkan harga terjadwal |
| GET | `/admin/weighings/lookup?qr_code=` | 🛡️ | Profil & saldo nasabah dari scan QR |
| POST | `/admin/weighings` | 🛡️ | Catat penimbangan (1 DB transaction) |
| GET | `/admin/weighings` | 🛡️ | Daftar penimbangan (filter tanggal & nasabah) |
| GET | `/admin/weighings/:id` | 🛡️ | Detail penimbangan + item |
| GET | `/admin/mutations` | 🛡️ | Mutasi saldo global (filter nasabah, tipe, sumber, periode) |
| GET | `/admin/audit-logs` | 🛡️ | Log aktivitas admin (`?actor=`, `?action=`, `?entity_id=`, `?from=&to=`) — hanya baca |
| GET | `/admin/audit-logs/actions` | 🛡️ | Daftar jenis aktivitas + label untuk filter |
| GET | `/admin/reports/summary` | 🛡️ | Ringkasan periode + tren harian (`?from=&to=`, default bulan berjalan) |
| GET | `/admin/reports/by-category` | 🛡️ | Rekap per kategori & jenis sampah + persentase |
| GET | `/admin/reports/by-nasabah` | 🛡️ | Rekap per nasabah (`?sort=`, `?search=`, `?all=true`) |
| GET | `/admin/reports/export.csv` | 🛡️ | Unduh CSV (`?type=by-nasabah\|by-category\|weighings\|mutations`, `?delimiter=comma`) |
| GET | `/admin/nasabah/:id/mutations` | 🛡️ | Saldo + mutasi satu nasabah |
| GET | `/nasabah/me/weighings` | 🎒 | Riwayat penimbangan pribadi |
| GET | `/nasabah/me/weighings/:id` | 🎒 | Detail penimbangan pribadi |
| GET | `/nasabah/me/mutations` | 🎒 | Riwayat saldo masuk/keluar (`?type=`, `?month=YYYY-MM`) |
| GET | `/nasabah/me/balance` | 🎒 | Ringkasan saldo untuk kartu saldo di Home |
| POST | `/nasabah/me/withdrawals` | 🎒 | Ajukan penarikan (min Rp10.000, ≤ saldo, 1 pengajuan aktif) |
| GET | `/nasabah/me/withdrawals` | 🎒 | Riwayat pengajuan penarikan (`?status=`) |
| GET | `/nasabah/me/withdrawals/:id` | 🎒 | Detail pengajuan |
| PATCH | `/nasabah/me/withdrawals/:id/cancel` | 🎒 | Batalkan pengajuan yang masih pending |
| GET | `/admin/withdrawals` | 🛡️ | Daftar pengajuan + jumlah per status (`?status=pending`) |
| GET | `/admin/withdrawals/:id` | 🛡️ | Detail pengajuan |
| PATCH | `/admin/withdrawals/:id/approve` | 🛡️ | Setujui (uang siap diambil) |
| PATCH | `/admin/withdrawals/:id/reject` | 🛡️ | Tolak — wajib `{"note": "alasan"}` |
| PATCH | `/admin/withdrawals/:id/complete` | 🛡️ | Uang diserahkan → saldo didebit |

### Struktur folder

```text
banksampahku-backend/
├── cmd/api/main.go        # titik masuk: konfigurasi, middleware, rute
├── internal/
│   ├── config/            # pembaca .env & logger
│   ├── database/          # koneksi, migrasi, seeder
│   ├── middleware/        # auth JWT, RBAC, audit log, rate limit
│   ├── auth/              # login, register, token, reset password
│   ├── nasabah/           # manajemen nasabah & QR Code
│   ├── waste/             # kategori, jenis sampah, harga dinamis
│   ├── weighing/          # transaksi penimbangan
│   ├── mutation/          # saldo & buku besar mutasi (satu-satunya pengubah saldo)
│   ├── withdrawal/        # pengajuan & verifikasi penarikan saldo
│   ├── report/            # laporan, agregasi, ekspor CSV
│   ├── notification/      # notifikasi in-app
│   ├── audit/             # pencatatan log aktivitas admin
│   ├── mailer/            # pengiriman email (SMTP / log)
│   └── utils/             # helper bersama (respons, validasi, password, dll.)
├── migrations/            # file SQL migrasi (di-embed ke binary)
├── docs/                  # dokumentasi
└── .env.example           # contoh konfigurasi
```
