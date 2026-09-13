package cache

import (
	"testing"
	"time"
)

func TestTTLAndBound(t *testing.T) {
	c := New[int](time.Minute, 2)
	c.Set("a", 1)
	c.Set("b", 2)
	c.Set("c", 3)
	if len(c.entries) != 2 {
		t.Fatal("unbounded cache")
	}
	if _, ok := c.Get("a"); ok {
		t.Fatal("oldest not evicted")
	}
	c.mu.Lock()
	c.entries["b"] = entry[int]{2, time.Now().Add(-time.Second)}
	c.mu.Unlock()
	if _, ok := c.Get("b"); ok {
		t.Fatal("expired value returned")
	}
}
