package store

import "time"

type Store interface {
	Incr(key string, ttl time.Duration, now time.Time) (int64, *time.Time, error)
	Close()
}
