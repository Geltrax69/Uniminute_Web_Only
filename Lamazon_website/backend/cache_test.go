package backend

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestReadCache(t *testing.T) {
	c := &readCache{entries: map[string]*cached{}}
	var n atomic.Int32
	calls := func() int { return int(n.Load()) }
	fetch := func(context.Context) ([]byte, error) { return []byte{byte('0' + n.Add(1))}, nil }
	ctx := context.Background()

	get := func() string { raw, _ := c.getRaw("tok", "/x", fetch, ctx); return string(raw) }
	if got := get(); got != "1" || calls() != 1 {
		t.Fatalf("first read = %q after %d calls", got, calls())
	}
	if got := get(); got != "1" || calls() != 1 {
		t.Fatalf("fresh read hit the API: %q after %d calls", got, calls())
	}

	// Stale: the old answer now, the refreshed one on the next read.
	c.entries[key("tok", "/x")].at = time.Now().Add(-fresh - time.Second)
	if got := get(); got != "1" {
		t.Fatalf("stale read = %q, want the cached answer", got)
	}
	for i := 0; i < 100 && calls() < 2; i++ {
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(5 * time.Millisecond)
	if got := get(); got != "2" {
		t.Fatalf("after refresh = %q, want 2", got)
	}

	// A write by the token forgets its reads.
	c.forget("tok")
	if got := get(); got != "3" {
		t.Fatalf("after forget = %q, want a new fetch", got)
	}

	// A fetch that began before a forget does not store what it read.
	slow := func(context.Context) ([]byte, error) { c.forget(""); return []byte("old"), nil }
	c.getRaw("", "/y", slow, ctx)
	if _, ok := c.entries[key("", "/y")]; ok {
		t.Fatal("a read that raced a write was cached")
	}
}
