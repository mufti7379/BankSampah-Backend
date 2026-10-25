package utils

import (
	"sync"
	"time"
)

type cacheEntry[V any] struct {
	value     V
	expiresAt time.Time
}

// TTLCache adalah cache in-memory sederhana dengan masa berlaku per entri
// (PRD Bab 8: cache data publik dengan TTL 60 detik). Aman dipakai bersamaan.
type TTLCache[K comparable, V any] struct {
	mu    sync.RWMutex
	items map[K]cacheEntry[V]
	ttl   time.Duration
}

// NewTTLCache membuat cache dengan masa berlaku ttl
func NewTTLCache[K comparable, V any](ttl time.Duration) *TTLCache[K, V] {
	return &TTLCache[K, V]{items: make(map[K]cacheEntry[V]), ttl: ttl}
}

// Get mengambil nilai yang belum kedaluwarsa
func (c *TTLCache[K, V]) Get(key K) (V, bool) {
	c.mu.RLock()
	e, ok := c.items[key]
	c.mu.RUnlock()

	if !ok || time.Now().After(e.expiresAt) {
		var zero V
		return zero, false
	}
	return e.value, true
}

// Set menyimpan nilai dengan masa berlaku ttl
func (c *TTLCache[K, V]) Set(key K, value V) {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	for k, e := range c.items { // bersihkan entri kedaluwarsa (jumlah entri kecil)
		if now.After(e.expiresAt) {
			delete(c.items, k)
		}
	}
	c.items[key] = cacheEntry[V]{value: value, expiresAt: now.Add(c.ttl)}
}

// Delete menghapus satu entri (dipanggil saat data berubah)
func (c *TTLCache[K, V]) Delete(key K) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.items, key)
}
