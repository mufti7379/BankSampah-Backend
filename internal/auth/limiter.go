package auth

import (
	"sync"
	"time"
)

// loginAttempt menyimpan jejak percobaan login gagal untuk satu email
type loginAttempt struct {
	failures    int
	firstFailAt time.Time
	lockedUntil time.Time
}

// LoginLimiter membatasi percobaan login gagal per email (PRD Bab 6A & 8):
// setelah `maxAttempts` kali gagal dalam jendela `lockDuration`, akun dikunci
// selama `lockDuration`. Penyimpanan in-memory (cukup untuk single instance;
// pindahkan ke Redis jika backend dijalankan multi-instance).
type LoginLimiter struct {
	mu           sync.Mutex
	attempts     map[string]*loginAttempt
	maxAttempts  int
	lockDuration time.Duration
	lastSweep    time.Time
}

// NewLoginLimiter membuat limiter dengan batas percobaan & durasi kunci (menit)
func NewLoginLimiter(maxAttempts, lockMinutes int) *LoginLimiter {
	return &LoginLimiter{
		attempts:     make(map[string]*loginAttempt),
		maxAttempts:  maxAttempts,
		lockDuration: time.Duration(lockMinutes) * time.Minute,
		lastSweep:    time.Now(),
	}
}

// LockedFor mengembalikan sisa durasi kunci untuk email (0 jika tidak terkunci)
func (l *LoginLimiter) LockedFor(email string) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()

	a, ok := l.attempts[email]
	if !ok {
		return 0
	}
	if remaining := time.Until(a.lockedUntil); remaining > 0 {
		return remaining
	}
	return 0
}

// RecordFailure mencatat login gagal. Mengembalikan durasi kunci jika batas tercapai.
func (l *LoginLimiter) RecordFailure(email string) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	l.sweep(now)

	a, ok := l.attempts[email]
	// Mulai jendela baru jika belum ada catatan atau jendela lama sudah lewat
	if !ok || now.Sub(a.firstFailAt) > l.lockDuration {
		a = &loginAttempt{firstFailAt: now}
		l.attempts[email] = a
	}

	a.failures++
	if a.failures >= l.maxAttempts {
		a.lockedUntil = now.Add(l.lockDuration)
		a.failures = 0
		a.firstFailAt = now
		return l.lockDuration
	}
	return 0
}

// Reset menghapus catatan gagal setelah login berhasil
func (l *LoginLimiter) Reset(email string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.attempts, email)
}

// sweep membersihkan entri kedaluwarsa secara berkala agar map tidak membengkak
func (l *LoginLimiter) sweep(now time.Time) {
	if now.Sub(l.lastSweep) < time.Minute {
		return
	}
	for email, a := range l.attempts {
		if now.After(a.lockedUntil) && now.Sub(a.firstFailAt) > l.lockDuration {
			delete(l.attempts, email)
		}
	}
	l.lastSweep = now
}
