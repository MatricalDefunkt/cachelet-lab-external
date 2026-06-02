// Package cache provides a concurrency-safe, in-memory key/value store with
// per-entry TTL and background eviction of expired entries.
package cache

import (
	"sync"
	"time"
)

// Store is an in-memory key/value cache with per-entry TTL. The zero value is
// not usable; create a Store with New. A Store is safe for concurrent use by
// multiple goroutines.
type Store struct {
	mu      sync.RWMutex
	entries map[string]entry

	stop     chan struct{}
	stopOnce sync.Once
}

type entry struct {
	value string
	// expiresAt is the absolute expiry time. A zero value means the entry
	// never expires.
	expiresAt time.Time
}

func (e entry) expired() bool {
	return !e.expiresAt.IsZero() && time.Now().After(e.expiresAt)
}

// New creates a Store. If cleanupInterval is positive, a background janitor
// evicts expired entries on that interval; otherwise expired entries are only
// removed lazily on access. Call Close to release the janitor goroutine.
func New(cleanupInterval time.Duration) *Store {
	s := &Store{
		entries: make(map[string]entry),
		stop:    make(chan struct{}),
	}
	if cleanupInterval > 0 {
		go s.janitor(cleanupInterval)
	}
	return s
}

func (s *Store) janitor(interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			s.evictExpired()
		case <-s.stop:
			return
		}
	}
}

// evictExpired removes all expired entries. It holds the write lock for the
// whole sweep, which is O(n) in the number of entries; fine at the current
// scale, but a source of latency spikes for very large maps.
func (s *Store) evictExpired() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, e := range s.entries {
		if e.expired() {
			delete(s.entries, k)
		}
	}
}

// Get returns the value stored under key. ok is false if the key is absent or
// has expired.
func (s *Store) Get(key string) (value string, ok bool) {
	s.mu.RLock()
	e, found := s.entries[key]
	s.mu.RUnlock()
	if !found || e.expired() {
		return "", false
	}
	return e.value, true
}

// Set stores value under key. A positive ttl gives the entry an expiry; a
// ttl <= 0 stores the entry without expiry. Setting an existing key replaces
// its value and expiry.
func (s *Store) Set(key, value string, ttl time.Duration) {
	var expiresAt time.Time
	if ttl > 0 {
		expiresAt = time.Now().Add(ttl)
	}
	s.mu.Lock()
	s.entries[key] = entry{value: value, expiresAt: expiresAt}
	s.mu.Unlock()
}

// Delete removes key from the store. It is a no-op if key is absent.
func (s *Store) Delete(key string) {
	s.mu.Lock()
	delete(s.entries, key)
	s.mu.Unlock()
}

// Len reports the number of entries currently held, including any that have
// expired but not yet been evicted by the janitor.
func (s *Store) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.entries)
}

// Close stops the background janitor. It is safe to call Close more than once.
func (s *Store) Close() {
	s.stopOnce.Do(func() { close(s.stop) })
}
