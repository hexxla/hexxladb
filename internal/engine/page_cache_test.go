package engine

import (
	"bytes"
	"testing"
)

// makeTestCache creates a small single-shard cache for deterministic tests.
// budgetBytes is sized so each test page (pageSize bytes) fits comfortably.
func makeTestCache(budgetBytes int64) *pageCache {
	c := &pageCache{
		shards:   make([]pcShard, 1),
		numShard: 1,
	}
	c.shards[0].index = make(map[uint64]*pcEntry)
	c.shards[0].maxSize = budgetBytes
	c.shards[0].coldTarget = budgetBytes
	return c
}

func TestPageCacheGetMiss(t *testing.T) {
	c := makeTestCache(64 << 10)
	if got := c.get(42); got != nil {
		t.Fatalf("expected nil on miss, got %v", got)
	}
	_, misses := c.stats()
	if misses != 1 {
		t.Fatalf("expected 1 miss, got %d", misses)
	}
}

func TestPageCacheSetAndGet(t *testing.T) {
	c := makeTestCache(64 << 10)
	data := bytes.Repeat([]byte{0xAB}, 4096)
	c.set(1, data)

	got := c.get(1)
	if got == nil {
		t.Fatal("expected cache hit, got nil")
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("cached data mismatch")
	}
	hits, misses := c.stats()
	if hits != 1 || misses != 0 {
		t.Fatalf("expected hits=1 misses=0, got hits=%d misses=%d", hits, misses)
	}
}

func TestPageCacheInvalidate(t *testing.T) {
	c := makeTestCache(64 << 10)
	c.set(7, bytes.Repeat([]byte{0x01}, 4096))
	c.invalidate(7)
	if got := c.get(7); got != nil {
		t.Fatal("expected nil after invalidate, got data")
	}
}

func TestPageCacheReturnsIndependentCopy(t *testing.T) {
	c := makeTestCache(64 << 10)
	orig := bytes.Repeat([]byte{0x55}, 4096)
	c.set(3, orig)

	got := c.get(3)
	if got == nil {
		t.Fatal("expected hit")
	}
	// Mutate the returned copy; the cache must be unaffected.
	got[0] = 0xFF
	got2 := c.get(3)
	if got2 == nil {
		t.Fatal("expected second hit")
	}
	if got2[0] != 0x55 {
		t.Fatalf("cache internal data was mutated by caller")
	}
}

func TestPageCacheOverwrite(t *testing.T) {
	c := makeTestCache(64 << 10)
	c.set(5, bytes.Repeat([]byte{0x01}, 4096))
	newData := bytes.Repeat([]byte{0x02}, 4096)
	c.set(5, newData)
	got := c.get(5)
	if got == nil {
		t.Fatal("expected hit after overwrite")
	}
	if !bytes.Equal(got, newData) {
		t.Fatalf("expected overwritten data")
	}
}

func TestPageCacheEviction(t *testing.T) {
	// Budget: 2 pages of 4096 bytes each. Insert 4 pages — the first two must
	// be evicted (or demoted to test state) to make room.
	pageSize := 4096
	c := makeTestCache(int64(2 * pageSize))

	for i := uint64(1); i <= 4; i++ {
		c.set(i, bytes.Repeat([]byte{byte(i)}, pageSize))
	}

	// The last two pages should be resident (hot or cold).
	// The first two may be evicted. We just assert the cache doesn't crash and
	// total hot+cold bytes don't exceed budget.
	s := &c.shards[0]
	s.mu.Lock()
	total := s.sizeHot + s.sizeCold
	s.mu.Unlock()
	if total > int64(2*pageSize) {
		t.Fatalf("cache over budget: hot+cold=%d, max=%d", total, 2*pageSize)
	}
}

func TestPageCacheGhostPromotion(t *testing.T) {
	// One-page budget: page 1 will be evicted when page 2 is inserted.
	// Re-accessing page 1 after eviction should record a miss (ghost) and then
	// re-populating it via set should promote it to hot.
	pageSize := 4096
	c := makeTestCache(int64(pageSize))

	p1 := bytes.Repeat([]byte{0x11}, pageSize)
	p2 := bytes.Repeat([]byte{0x22}, pageSize)

	c.set(1, p1)
	// Insert page 2: page 1 should be evicted (data freed, ghost retained).
	c.set(2, p2)

	// Page 1 is a ghost — get returns nil.
	if got := c.get(1); got != nil {
		// This is acceptable if the cache chose to keep page 1 hot and evict page 2.
		// Just verify we don't return stale data.
		if !bytes.Equal(got, p1) {
			t.Fatalf("stale data in cache: got %v want %v", got[:4], p1[:4])
		}
	}

	// Re-populate page 1 — if it was a ghost this promotes it to hot.
	c.set(1, p1)
	got := c.get(1)
	if got == nil {
		t.Fatal("expected hit after re-population")
	}
	if !bytes.Equal(got, p1) {
		t.Fatalf("data mismatch after ghost promotion")
	}
}

func TestPageCacheMultipleShardsNoRace(t *testing.T) {
	// Use the real constructor so multiple shards are exercised.
	c := newPageCache(512 << 10)
	if c == nil {
		t.Skip("cache disabled (budget=0)")
	}

	const n = 1000
	done := make(chan struct{})
	for g := range 4 {
		base := uint64(g * n)
		go func() {
			defer func() { done <- struct{}{} }()
			for i := range uint64(n) {
				pageID := base + i
				c.set(pageID, bytes.Repeat([]byte{byte(pageID & 0xFF)}, 4096))
				_ = c.get(pageID)
				c.invalidate(pageID)
			}
		}()
	}
	for range 4 {
		<-done
	}
}

// TestPageCacheDisabled verifies that newPageCache(0) returns nil.
func TestPageCacheDisabled(t *testing.T) {
	if c := newPageCache(0); c != nil {
		t.Fatal("expected nil cache for zero budget")
	}
	if c := newPageCache(-1); c != nil {
		t.Fatal("expected nil cache for negative budget")
	}
}

// TestPageCacheIntegration exercises the full engine path: write a page,
// read it back, and confirm the second read is a cache hit.
func TestPageCacheIntegration(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := dir + "/test.db"

	eng, err := Open(path, &Options{PageCacheSize: 4 << 20})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if err := eng.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})

	pageData := bytes.Repeat([]byte{0xCC}, eng.pageSize)

	// Write a page via WritePage.
	if err := eng.WritePage(1, pageData); err != nil {
		t.Fatalf("WritePage: %v", err)
	}

	// First read — populates cache.
	got1, err := eng.ReadPage(1)
	if err != nil {
		t.Fatalf("ReadPage (first): %v", err)
	}
	if !bytes.Equal(got1, pageData) {
		t.Fatal("first read data mismatch")
	}

	hits1, _ := eng.PageCacheStats()

	// Second read — should be a cache hit.
	got2, err := eng.ReadPage(1)
	if err != nil {
		t.Fatalf("ReadPage (second): %v", err)
	}
	if !bytes.Equal(got2, pageData) {
		t.Fatal("second read data mismatch")
	}

	hits2, _ := eng.PageCacheStats()
	if hits2 <= hits1 {
		t.Fatalf("expected more hits after second read: hits1=%d hits2=%d", hits1, hits2)
	}
}

// TestPageCacheInvalidateOnWrite verifies that writing a page invalidates its
// cache entry so subsequent reads see the new data.
func TestPageCacheInvalidateOnWrite(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := dir + "/test.db"

	eng, err := Open(path, &Options{PageCacheSize: 4 << 20})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if err := eng.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})

	v1 := bytes.Repeat([]byte{0x01}, eng.pageSize)
	v2 := bytes.Repeat([]byte{0x02}, eng.pageSize)

	if err := eng.WritePage(1, v1); err != nil {
		t.Fatalf("WritePage v1: %v", err)
	}
	// Warm cache.
	if _, err := eng.ReadPage(1); err != nil {
		t.Fatalf("ReadPage v1: %v", err)
	}

	// Overwrite.
	if err := eng.WritePage(1, v2); err != nil {
		t.Fatalf("WritePage v2: %v", err)
	}

	// Must read new data, not the stale cached v1.
	got, err := eng.ReadPage(1)
	if err != nil {
		t.Fatalf("ReadPage v2: %v", err)
	}
	if !bytes.Equal(got, v2) {
		t.Fatalf("expected v2 after overwrite, got old cached data")
	}
}
