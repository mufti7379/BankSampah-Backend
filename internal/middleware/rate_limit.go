package middleware

import (
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
	"golang.org/x/time/rate"

	"github.com/banksampahku/backend/internal/utils"
)

// visitor menyimpan token bucket milik satu IP
type visitor struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// IPRateLimiter membatasi jumlah request per IP memakai algoritma token bucket:
// rata-rata `perMinute` request/menit dengan lonjakan (burst) maksimal `burst`.
// Penyimpanan in-memory (cukup untuk single instance; pakai Redis jika multi-instance).
type IPRateLimiter struct {
	name     string
	mu       sync.Mutex
	visitors map[string]*visitor
	limit    rate.Limit
	burst    int
	perMin   int
}

// visitorTTL: IP yang tidak aktif lebih lama dari ini dihapus dari memori
const visitorTTL = 5 * time.Minute

// NewIPRateLimiter membuat limiter per IP dan menjalankan pembersih memori di background
func NewIPRateLimiter(name string, perMinute, burst int) *IPRateLimiter {
	if perMinute <= 0 {
		perMinute = 1
	}
	if burst <= 0 {
		burst = perMinute
	}
	l := &IPRateLimiter{
		name:     name,
		visitors: make(map[string]*visitor),
		limit:    rate.Every(time.Minute / time.Duration(perMinute)),
		burst:    burst,
		perMin:   perMinute,
	}
	go l.cleanupLoop()
	return l
}

// Middleware mengembalikan gin handler; request yang melebihi batas mendapat 429
func (l *IPRateLimiter) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := c.ClientIP()
		reservation := l.getLimiter(ip).Reserve()

		c.Header("X-RateLimit-Limit", strconv.Itoa(l.perMin))

		if delay := reservation.Delay(); delay > 0 {
			reservation.Cancel() // kembalikan token, request ini tidak diproses
			retryAfter := int(math.Ceil(delay.Seconds()))
			c.Header("Retry-After", strconv.Itoa(retryAfter))
			log.Warn().Str("limiter", l.name).Str("ip", ip).Str("path", c.Request.URL.Path).
				Msg("Rate limit terlampaui")
			utils.RespondError(c, http.StatusTooManyRequests,
				"Terlalu banyak permintaan, silakan coba lagi dalam "+strconv.Itoa(retryAfter)+" detik")
			return
		}
		c.Next()
	}
}

// getLimiter mengambil (atau membuat) token bucket untuk IP tertentu
func (l *IPRateLimiter) getLimiter(ip string) *rate.Limiter {
	l.mu.Lock()
	defer l.mu.Unlock()

	v, ok := l.visitors[ip]
	if !ok {
		v = &visitor{limiter: rate.NewLimiter(l.limit, l.burst)}
		l.visitors[ip] = v
	}
	v.lastSeen = time.Now()
	return v.limiter
}

// cleanupLoop menghapus IP yang sudah lama tidak aktif agar memori tidak membengkak
func (l *IPRateLimiter) cleanupLoop() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		l.mu.Lock()
		for ip, v := range l.visitors {
			if time.Since(v.lastSeen) > visitorTTL {
				delete(l.visitors, ip)
			}
		}
		l.mu.Unlock()
	}
}
