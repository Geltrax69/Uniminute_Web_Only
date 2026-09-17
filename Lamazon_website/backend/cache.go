package backend

// cache.go — stale-while-revalidate for GETs. Every read from the API is a
// ~250ms round trip, and a page makes several, so a click waited over a
// second for data that almost never changed in between. A cached answer is
// served at once; one older than fresh is served too while a background
// request replaces it, so the next click sees the update.

import (
	"context"
	"strings"
	"sync"
	"time"
)

const (
	fresh = 15 * time.Second // served without asking the API again
	stale = 10 * time.Minute // served while a refresh runs; older is refetched in line
)

type cached struct {
	raw        []byte
	at         time.Time
	refreshing bool
}

type readCache struct {
	mu      sync.Mutex
	entries map[string]*cached
	// gen moves on every forget, so a fetch that began before a write cannot
	// put what it read back afterwards.
	gen int
}

// key keeps a signed-in shopper's reads apart from everyone else's.
func key(token, path string) string { return token + "\x00" + path }

// Forget is forget for writes made around the client, such as the proxies.
func (b *Backend) Forget(token string) { b.cache.forget(token) }

// forget drops a token's cached reads after that token wrote something, so a
// shopper sees their own change straight away. "" also drops the public reads.
func (c *readCache) forget(token string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.gen++
	for k := range c.entries {
		if strings.HasPrefix(k, token+"\x00") || token == "" {
			delete(c.entries, k)
		}
	}
}

// getRaw answers from the cache when it can, and otherwise asks fetch.
func (c *readCache) getRaw(token, path string, fetch func(context.Context) ([]byte, error), ctx context.Context) ([]byte, error) {
	k := key(token, path)
	c.mu.Lock()
	gen := c.gen
	e := c.entries[k]
	if e != nil {
		age := time.Since(e.at)
		if age < fresh {
			c.mu.Unlock()
			return e.raw, nil
		}
		if age < stale {
			if !e.refreshing {
				e.refreshing = true
				// Detached: the refresh must outlive the request that noticed it.
				go func() {
					raw, err := fetch(context.Background())
					c.mu.Lock()
					defer c.mu.Unlock()
					if err == nil && c.gen == gen {
						c.entries[k] = &cached{raw: raw, at: time.Now()}
					} else if cur := c.entries[k]; cur != nil {
						cur.refreshing = false
					}
				}()
			}
			c.mu.Unlock()
			return e.raw, nil
		}
	}
	c.mu.Unlock()

	raw, err := fetch(ctx)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	if c.gen == gen {
		c.entries[k] = &cached{raw: raw, at: time.Now()}
	}
	c.mu.Unlock()
	return raw, nil
}
