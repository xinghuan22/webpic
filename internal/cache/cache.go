package cache

import (
	"sync"
	"time"
)

type entry[T any] struct {
	value   T
	expires time.Time
}
type Cache[T any] struct {
	mu      sync.Mutex
	entries map[string]entry[T]
	ttl     time.Duration
	max     int
}

func New[T any](ttl time.Duration, max int) *Cache[T] {
	return &Cache[T]{entries: make(map[string]entry[T]), ttl: ttl, max: max}
}
func (c *Cache[T]) Get(key string) (T, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if ok && time.Now().Before(e.expires) {
		return e.value, true
	}
	delete(c.entries, key)
	var zero T
	return zero, false
}
func (c *Cache[T]) Set(key string, v T) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.max <= 0 {
		return
	}
	if len(c.entries) >= c.max {
		oldest := ""
		var at time.Time
		for k, e := range c.entries {
			if oldest == "" || e.expires.Before(at) {
				oldest = k
				at = e.expires
			}
		}
		delete(c.entries, oldest)
	}
	c.entries[key] = entry[T]{v, time.Now().Add(c.ttl)}
}
