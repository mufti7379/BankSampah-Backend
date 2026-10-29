package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/banksampahku/backend/internal/audit"
	"github.com/banksampahku/backend/internal/config"
	"github.com/banksampahku/backend/internal/database"
	"github.com/banksampahku/backend/internal/utils"
)

// Integration test Task 4.2: memicu aksi admin lewat router sungguhan (newRouter)
// di atas PostgreSQL, lalu memverifikasi GET /api/v1/admin/audit-logs.
//
// TEST_DATABASE_URL berisi koneksi ke server PostgreSQL (database apa saja) dengan
// hak CREATEDB. Test membuat database sementara, menjalankan migrasi + seeder, lalu
// menghapusnya kembali, jadi data yang sudah ada tidak tersentuh.
const testDatabaseURLEnv = "TEST_DATABASE_URL"

// Akun dari seeder (internal/database/seed.go)
const (
	seedAdminEmail      = "admin@banksampahku.id"
	seedAdminPassword   = "Admin#2025"
	seedNasabahEmail    = "aisyah@banksampahku.id"
	seedNasabahPassword = "Nasabah#2025"
	seedNasabahQR       = "BSK-7HQ2D9LM"
)

// testEnv adalah API lengkap di atas database sementara
type testEnv struct {
	db     *gorm.DB
	router http.Handler
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	baseURL := os.Getenv(testDatabaseURLEnv)
	if baseURL == "" {
		t.Skipf("%s belum diisi, integration test dilewati", testDatabaseURLEnv)
	}
	if testing.Short() {
		t.Skip("integration test dilewati pada mode -short")
	}

	gin.SetMode(gin.TestMode)
	prevLevel := zerolog.GlobalLevel()
	zerolog.SetGlobalLevel(zerolog.ErrorLevel) // log request & migrasi tidak memenuhi output test
	t.Cleanup(func() { zerolog.SetGlobalLevel(prevLevel) })

	maint, err := gorm.Open(postgres.Open(baseURL), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatalf("gagal terhubung ke %s: %v", testDatabaseURLEnv, err)
	}
	suffix := make([]byte, 4)
	_, _ = rand.Read(suffix)
	name := "banksampahku_it_" + hex.EncodeToString(suffix)
	if err := maint.Exec("CREATE DATABASE " + name).Error; err != nil {
		t.Fatalf("gagal membuat database sementara: %v", err)
	}
	t.Cleanup(func() {
		_ = database.Close()
		if err := maint.Exec("DROP DATABASE IF EXISTS " + name + " WITH (FORCE)").Error; err != nil {
			t.Errorf("gagal menghapus database sementara %s: %v", name, err)
		}
		if sqlDB, err := maint.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})

	u, err := url.Parse(baseURL)
	if err != nil {
		t.Fatalf("%s tidak valid: %v", testDatabaseURLEnv, err)
	}
	u.Path = "/" + name
	dbURL := u.String()

	if err := database.MigrateUp(dbURL); err != nil {
		t.Fatalf("migrasi gagal: %v", err)
	}
	conn, err := database.Connect(dbURL, false)
	if err != nil {
		t.Fatalf("gagal terhubung ke database sementara: %v", err)
	}
	// Query yang sengaja ditolak (misal trigger append-only) tidak perlu dicetak GORM
	db := conn.Session(&gorm.Session{Logger: logger.Discard})

	hasher := utils.NewPasswordHasher("pepper-integration-test")
	if err := database.Seed(db, hasher); err != nil {
		t.Fatalf("seeder gagal: %v", err)
	}

	router, err := newRouter(&config.Config{
		AppEnv:                  "test",
		CORSAllowedOrigins:      []string{"http://localhost:5173"},
		JWTSecret:               "jwt-secret-integration-test-minimal-32-karakter",
		JWTExpiredHours:         1,
		RateLimitPerMinute:      1000,
		AuthRateLimitPerMinute:  1000,
		LoginAttemptLimit:       5,
		LoginLockMinutes:        15,
		QRPrefix:                "BSK-",
		PasswordResetURL:        "http://localhost:5173/reset-password",
		PasswordResetTTLMinutes: 30,
	}, db, hasher)
	if err != nil {
		t.Fatalf("gagal menyiapkan router: %v", err)
	}
	return &testEnv{db: db, router: router}
}

// apiResult adalah envelope respons API {success, message, data, errors}
type apiResult struct {
	Code    int
	Message string            `json:"message"`
	Data    json.RawMessage   `json:"data"`
	Errors  map[string]string `json:"errors"`
}

// call mengirim request ke router; headers berisi pasangan nama, nilai
func (e *testEnv) call(t *testing.T, method, path, token string, body any, headers ...string) apiResult {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, "/api/v1"+path, &buf)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Client-Platform", "mobile") // token login dikirim di body
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	w := httptest.NewRecorder()
	e.router.ServeHTTP(w, req)

	res := apiResult{Code: w.Code}
	// Route yang tidak terdaftar dijawab Gin dengan teks biasa ("404 page not found")
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		res.Message = w.Body.String()
	}
	return res
}

// must menghentikan test jika status HTTP tidak sesuai (langkah berikutnya bergantung padanya)
func must(t *testing.T, want int, r apiResult, step string) apiResult {
	t.Helper()
	if r.Code != want {
		t.Fatalf("%s: status = %d, want %d (%s %v)", step, r.Code, want, r.Message, r.Errors)
	}
	return r
}

func decode[T any](t *testing.T, raw json.RawMessage) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("gagal decode data: %v\n%s", err, raw)
	}
	return v
}

type idOnly struct {
	ID string `json:"id"`
}

func (e *testEnv) login(t *testing.T, email, password string) (token, userID string) {
	t.Helper()
	r := must(t, http.StatusOK, e.call(t, http.MethodPost, "/auth/login", "",
		map[string]string{"email": email, "password": password}), "login "+email)
	d := decode[struct {
		AccessToken string `json:"access_token"`
		User        idOnly `json:"user"`
	}](t, r.Data)
	return d.AccessToken, d.User.ID
}

func newUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

func TestAuditTrailIntegration(t *testing.T) {
	env := newTestEnv(t)
	now := time.Now()
	today := now.Format("2006-01-02")
	tomorrow := now.AddDate(0, 0, 1).Format("2006-01-02")
	yesterday := now.AddDate(0, 0, -1).Format("2006-01-02")

	adminToken, adminID := env.login(t, seedAdminEmail, seedAdminPassword)
	nasabahToken, _ := env.login(t, seedNasabahEmail, seedNasabahPassword)

	// ---- Memicu 7 aksi wajib, diselingi aksi yang TIDAK boleh tercatat ----
	types := decode[utils.PaginatedData[idOnly]](t, must(t, http.StatusOK,
		env.call(t, http.MethodGet, "/admin/waste-types?limit=1", adminToken, nil), "daftar jenis sampah").Data)
	if len(types.Items) == 0 {
		t.Fatal("seeder tidak menghasilkan jenis sampah")
	}
	typeID := types.Items[0].ID

	must(t, http.StatusCreated, env.call(t, http.MethodPost, "/admin/waste-types/"+typeID+"/prices", adminToken,
		map[string]any{"price_per_unit": 5000, "effective_date": tomorrow}), audit.ActionPriceUpdate)

	newNasabah := decode[idOnly](t, must(t, http.StatusCreated, env.call(t, http.MethodPost, "/admin/nasabah", adminToken,
		map[string]any{"full_name": "Citra Lestari", "email": "citra@banksampahku.id"}), audit.ActionNasabahCreate).Data)

	// 100 kg × harga seed termurah (Rp200/kg) = minimal Rp20.000, cukup untuk dua penarikan Rp10.000
	weighingBody := map[string]any{
		"qr_code": seedNasabahQR,
		"items":   []map[string]any{{"waste_type_id": typeID, "weight": 100}},
	}
	key := newUUID()
	weighing := decode[idOnly](t, must(t, http.StatusCreated, env.call(t, http.MethodPost, "/admin/weighings", adminToken,
		weighingBody, "Idempotency-Key", key), audit.ActionWeighingCreate).Data)
	must(t, http.StatusOK, env.call(t, http.MethodPost, "/admin/weighings", adminToken,
		weighingBody, "Idempotency-Key", key), "kirim ulang penimbangan (Idempotency-Key sama)")
	must(t, http.StatusUnprocessableEntity, env.call(t, http.MethodPost, "/admin/weighings", adminToken,
		map[string]any{"qr_code": seedNasabahQR, "items": []map[string]any{{"waste_type_id": newUUID(), "weight": 1}}}),
		"penimbangan dengan jenis sampah tidak dikenal")

	first := decode[idOnly](t, must(t, http.StatusCreated, env.call(t, http.MethodPost, "/nasabah/me/withdrawals",
		nasabahToken, map[string]any{"amount": 10000}), "pengajuan penarikan #1").Data)
	must(t, http.StatusOK, env.call(t, http.MethodPatch, "/admin/withdrawals/"+first.ID+"/approve", adminToken, nil),
		audit.ActionWithdrawalApprove)
	must(t, http.StatusOK, env.call(t, http.MethodPatch, "/admin/withdrawals/"+first.ID+"/complete", adminToken, nil),
		audit.ActionWithdrawalComplete)
	must(t, http.StatusConflict, env.call(t, http.MethodPatch, "/admin/withdrawals/"+first.ID+"/complete", adminToken, nil),
		"menyelesaikan ulang penarikan")

	second := decode[idOnly](t, must(t, http.StatusCreated, env.call(t, http.MethodPost, "/nasabah/me/withdrawals",
		nasabahToken, map[string]any{"amount": 10000}), "pengajuan penarikan #2").Data)
	must(t, http.StatusOK, env.call(t, http.MethodPatch, "/admin/withdrawals/"+second.ID+"/reject", adminToken,
		map[string]any{"note": "Data tidak sesuai"}), audit.ActionWithdrawalReject)

	must(t, http.StatusOK, env.call(t, http.MethodPatch, "/admin/nasabah/"+newNasabah.ID+"/deactivate", adminToken, nil),
		audit.ActionUserDeactivate)
	must(t, http.StatusConflict, env.call(t, http.MethodPatch, "/admin/nasabah/"+newNasabah.ID+"/deactivate", adminToken, nil),
		"menonaktifkan ulang nasabah")

	// Log milik admin sekolah lain tidak boleh terlihat oleh admin sekolah ini
	if err := env.db.Exec(`
		WITH s AS (INSERT INTO schools (name) VALUES ('SMP Lain') RETURNING id),
		     u AS (INSERT INTO users (school_id, email, password_hash, full_name, role)
		           SELECT id, 'admin@smplain.id', 'x', 'Sari Lain', 'admin' FROM s RETURNING id)
		INSERT INTO audit_logs (actor_id, action, entity) SELECT id, ?, ? FROM u`,
		audit.ActionWeighingCreate, audit.EntityWeighing).Error; err != nil {
		t.Fatalf("gagal menyiapkan log sekolah lain: %v", err)
	}

	listLogs := func(t *testing.T, query string) utils.PaginatedData[audit.LogItem] {
		t.Helper()
		r := must(t, http.StatusOK, env.call(t, http.MethodGet, "/admin/audit-logs?"+query, adminToken, nil),
			"GET /admin/audit-logs?"+query)
		return decode[utils.PaginatedData[audit.LogItem]](t, r.Data)
	}

	t.Run("tujuh aksi wajib tercatat tepat sekali", func(t *testing.T) {
		logs := listLogs(t, "limit=100")
		counts := map[string]int{}
		for _, l := range logs.Items {
			counts[l.Action]++
			if l.Actor == nil || l.Actor.ID != adminID {
				t.Errorf("%s: actor = %+v, want admin %s", l.Action, l.Actor, adminID)
			}
			if l.EntityID == nil || l.IPAddress == nil || string(l.Metadata) == "null" {
				t.Errorf("%s: entity_id/ip_address/metadata kosong", l.Action)
			}
			if l.ActionLabel == l.Action {
				t.Errorf("%s: belum punya label", l.Action)
			}
		}
		for _, action := range []string{
			audit.ActionWeighingCreate, audit.ActionPriceUpdate, audit.ActionNasabahCreate,
			audit.ActionWithdrawalApprove, audit.ActionWithdrawalReject, audit.ActionWithdrawalComplete,
			audit.ActionUserDeactivate,
		} {
			if counts[action] != 1 {
				t.Errorf("%s tercatat %d kali, want 1", action, counts[action])
			}
		}
		// Aksi gagal, kirim ulang penimbangan, & log sekolah lain tidak ikut terhitung
		if logs.Meta.Total != 7 {
			t.Errorf("total = %d, want 7 (%v)", logs.Meta.Total, counts)
		}
		if len(logs.Items) > 0 && logs.Items[0].Action != audit.ActionUserDeactivate {
			t.Errorf("log teratas = %s, want aksi terakhir %s", logs.Items[0].Action, audit.ActionUserDeactivate)
		}
	})

	t.Run("metadata menyimpan before/after", func(t *testing.T) {
		price := listLogs(t, "action="+audit.ActionPriceUpdate).Items
		deactivate := listLogs(t, "action="+audit.ActionUserDeactivate).Items
		if len(price) != 1 || len(deactivate) != 1 {
			t.Fatalf("log PRICE_UPDATE = %d, USER_DEACTIVATE = %d, want 1 & 1", len(price), len(deactivate))
		}
		var pm struct {
			Before, After map[string]any
		}
		var dm struct {
			Status struct{ Before, After string }
		}
		if err := json.Unmarshal(price[0].Metadata, &pm); err != nil || pm.Before == nil ||
			pm.After["price_per_unit"] != float64(5000) || pm.After["effective_date"] != tomorrow {
			t.Errorf("metadata PRICE_UPDATE = %s", price[0].Metadata)
		}
		if err := json.Unmarshal(deactivate[0].Metadata, &dm); err != nil ||
			dm.Status.Before != "active" || dm.Status.After != "inactive" {
			t.Errorf("metadata USER_DEACTIVATE = %s", deactivate[0].Metadata)
		}
	})

	t.Run("filter", func(t *testing.T) {
		cases := []struct {
			name, query string
			want        int64
		}{
			{"action huruf kecil", "action=weighing_create", 1},
			{"action", "action=" + audit.ActionWithdrawalApprove, 1},
			{"actor UUID", "actor=" + adminID, 7},
			{"actor potongan nama", "actor=wulan", 7},
			{"actor sekolah lain", "actor=" + url.QueryEscape("Sari Lain"), 0},
			{"actor wildcard di-escape", "actor=%25", 0},
			{"entity", "entity=" + audit.EntityWithdrawal, 3},
			{"entity_id", "entity_id=" + weighing.ID, 1},
			{"rentang hari ini", "from=" + today + "&to=" + today, 7},
			{"mulai besok", "from=" + tomorrow, 0},
			{"sampai kemarin", "to=" + yesterday, 0},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				if got := listLogs(t, tc.query).Meta.Total; got != tc.want {
					t.Errorf("total = %d, want %d", got, tc.want)
				}
			})
		}
	})

	t.Run("pagination", func(t *testing.T) {
		seen := map[string]bool{}
		for page, wantItems := range map[int]int{1: 3, 2: 3, 3: 1, 4: 0} {
			res := listLogs(t, fmt.Sprintf("page=%d&limit=3", page))
			if len(res.Items) != wantItems || res.Meta.Page != page || res.Meta.TotalPages != 3 || res.Meta.Total != 7 {
				t.Errorf("page %d: %d item, meta %+v; want %d item, 3 halaman", page, len(res.Items), res.Meta, wantItems)
			}
			for _, l := range res.Items {
				if seen[l.ID] {
					t.Errorf("log %s muncul di lebih dari satu halaman", l.ID)
				}
				seen[l.ID] = true
			}
		}
		if len(seen) != 7 {
			t.Errorf("gabungan semua halaman = %d log, want 7", len(seen))
		}
	})

	t.Run("query tidak valid ditolak", func(t *testing.T) {
		for _, query := range []string{
			"from=" + tomorrow + "&to=" + today, // from > to
			"from=29-10-2026",
			"limit=101",
			"entity_id=bukan-uuid",
		} {
			if r := env.call(t, http.MethodGet, "/admin/audit-logs?"+query, adminToken, nil); r.Code != http.StatusUnprocessableEntity {
				t.Errorf("%s: status = %d, want 422", query, r.Code)
			}
		}
	})

	t.Run("hanya admin & tanpa endpoint ubah/hapus", func(t *testing.T) {
		logID := listLogs(t, "limit=1").Items[0].ID
		cases := []struct {
			name, method, path, token string
			want                      int
		}{
			{"nasabah", http.MethodGet, "/admin/audit-logs", nasabahToken, http.StatusForbidden},
			{"tanpa token", http.MethodGet, "/admin/audit-logs", "", http.StatusUnauthorized},
			{"hapus log", http.MethodDelete, "/admin/audit-logs/" + logID, adminToken, http.StatusNotFound},
			{"ubah log", http.MethodPatch, "/admin/audit-logs/" + logID, adminToken, http.StatusNotFound},
		}
		for _, tc := range cases {
			if r := env.call(t, tc.method, tc.path, tc.token, nil); r.Code != tc.want {
				t.Errorf("%s: status = %d, want %d", tc.name, r.Code, tc.want)
			}
		}
		actions := decode[[]audit.ActionOption](t, must(t, http.StatusOK,
			env.call(t, http.MethodGet, "/admin/audit-logs/actions", adminToken, nil), "daftar aksi").Data)
		if len(actions) != len(audit.ActionOptions()) {
			t.Errorf("daftar aksi = %d, want %d", len(actions), len(audit.ActionOptions()))
		}
	})

	t.Run("append-only di level database", func(t *testing.T) {
		for _, q := range []string{
			"UPDATE audit_logs SET action = 'DIUBAH'",
			"DELETE FROM audit_logs",
			"DELETE FROM audit_logs WHERE false", // tidak mengenai baris pun tetap ditolak
			"TRUNCATE audit_logs",
		} {
			if err := env.db.Exec(q).Error; err == nil || !strings.Contains(err.Error(), "append-only") {
				t.Errorf("%q harus ditolak trigger, err = %v", q, err)
			}
		}
		var total int64
		if err := env.db.Raw("SELECT COUNT(*) FROM audit_logs").Scan(&total).Error; err != nil {
			t.Fatal(err)
		}
		if total != 8 { // 7 aksi + 1 log sekolah lain
			t.Errorf("jumlah log = %d, want 8 (tidak ada yang berubah/terhapus)", total)
		}
	})
}
