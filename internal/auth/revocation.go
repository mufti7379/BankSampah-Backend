package auth

import (
	"sync"
	"time"
)

// RevocationStore menyimpan jti token yang sudah logout hingga token tersebut
// kedaluwarsa, sehingga token lama tidak bisa dipakai lagi. Penyimpanan in-memory
// (hilang saat restart; pindahkan ke Redis/DB jika butuh persistensi multi-instance).
type RevocationStore struct {
	mu        sync.Mutex
	revoked   map[string]time.Time // jti -> waktu kedaluwarsa token
	lastSweep time.Time
}

// NewRevocationStore membuat store pencabutan token
func NewRevocationStore() *RevocationStore {
	return &RevocationStore{revoked: make(map[string]time.Time), lastSweep: time.Now()}
}

// Revoke mencabut token dengan jti tertentu sampai expiresAt
func (s *RevocationStore) Revoke(jti string, expiresAt time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	if now.Sub(s.lastSweep) > time.Minute {
		for id, exp := range s.revoked {
			if now.After(exp) {
				delete(s.revoked, id)
			}
		}
		s.lastSweep = now
	}
	s.revoked[jti] = expiresAt
}

// IsRevoked mengecek apakah token dengan jti tertentu sudah dicabut
func (s *RevocationStore) IsRevoked(jti string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	exp, ok := s.revoked[jti]
	return ok && time.Now().Before(exp)
}
