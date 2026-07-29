package store

import (
	"errors"
	"sync"
	"time"
)

const memoryCleanupInterval = time.Minute

var errMemoryStoreClosed = errors.New("memory store: closed")

type memoryEntry struct {
	count    int64
	expireAt time.Time
}

type MemoryStore struct {
	mu          sync.Mutex
	entries     map[string]memoryEntry
	nextCleanup time.Time
	closed      bool
}

var _ Store = (*MemoryStore)(nil)

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		entries: make(map[string]memoryEntry),
	}
}

func (s *MemoryStore) Incr(key string, ttl time.Duration, now time.Time) (int64, *time.Time, error) {
	ttlSec := int64(ttl / time.Second)
	if ttlSec < 1 {
		ttlSec = 1
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return 0, nil, errMemoryStoreClosed
	}

	if s.nextCleanup.IsZero() || !now.Before(s.nextCleanup) {
		for entryKey, entry := range s.entries {
			if !now.Before(entry.expireAt) {
				delete(s.entries, entryKey)
			}
		}
		s.nextCleanup = now.Add(memoryCleanupInterval)
	}

	entry, ok := s.entries[key]
	if !ok || !now.Before(entry.expireAt) {
		entry = memoryEntry{
			count:    1,
			expireAt: now.Add(time.Duration(ttlSec) * time.Second),
		}
	} else {
		entry.count++
	}
	s.entries[key] = entry

	expireAt := entry.expireAt
	return entry.count, &expireAt, nil
}

func (s *MemoryStore) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return
	}
	s.entries = nil
	s.closed = true
}
