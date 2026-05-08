package engine

import (
	"runtime"
	"sync"
	"sync/atomic"
)

// pageCache is a sharded CLOCK-Pro page cache.
//
// Each shard runs an independent CLOCK-Pro eviction loop with three entry
// states — hot, cold, and test (ghost). Ghost entries retain the page key
// after eviction so that a re-access promotes the page directly to hot and
// widens the cold budget. This prevents scan pollution: sequential
// AscendRange walks that access many leaf pages once do not evict the hot
// working set of internal B+ tree nodes.
//
// Key is a pageID (uint64). Value is a copy of the raw page bytes (plaintext
// after transformRead / AfterRead decryption). The cache stores the
// post-transform bytes so that cache hits bypass both the pread syscall and
// the decryption step.
//
// Thread-safety: each shard has its own mutex. Concurrent readers and the
// single writer (which invalidates on commit) never contend across shards
// unless they hash to the same shard.
type pageCache struct {
	shards   []pcShard
	numShard int

	// hits and misses are updated atomically for observability.
	hits   atomic.Int64
	misses atomic.Int64
}

const (
	pcEntryHot  = 0
	pcEntryHot2 = 1 // hot entry that was referenced again (stays hot)
	pcEntryCold = 2
	pcEntryTest = 3 // ghost: data freed, key retained
)

type pcEntry struct {
	pageID     uint64
	data       []byte // nil for ghost (test) entries
	size       int64
	ptype      int8 // pcEntryHot / pcEntryCold / pcEntryTest
	referenced bool // CLOCK reference bit

	// doubly-linked ring for clock hand walk
	prev, next *pcEntry

	// per-file doubly-linked list (not used here; kept for future EvictFile)
}

type pcShard struct {
	mu sync.Mutex

	// index for O(1) lookup
	index map[uint64]*pcEntry

	// three clock hands
	handHot  *pcEntry
	handCold *pcEntry
	handTest *pcEntry

	sizeHot  int64
	sizeCold int64
	sizeTest int64

	countHot  int
	countCold int
	countTest int

	coldTarget int64 // target bytes for cold set
	maxSize    int64 // bytes budget for this shard (hot + cold only)
}

// newPageCache creates a pageCache with the given total byte budget. Returns
// nil if budget == 0 (cache disabled). Shard count is
// min(4*GOMAXPROCS, 16), same ratio as Pebble's internal/cache.
func newPageCache(budgetBytes int64) *pageCache {
	if budgetBytes <= 0 {
		return nil
	}
	n := max(min(4*runtime.GOMAXPROCS(0), 16), 1)
	shardSize := max(budgetBytes/int64(n), 1)
	c := &pageCache{
		shards:   make([]pcShard, n),
		numShard: n,
	}
	for i := range c.shards {
		c.shards[i].index = make(map[uint64]*pcEntry)
		c.shards[i].maxSize = shardSize
		c.shards[i].coldTarget = shardSize
	}
	return c
}

// fibonacciHash maps pageID to a shard index using Knuth's multiplicative
// hash constant (same constant as Pebble clockpro.go:55).
func (c *pageCache) shardFor(pageID uint64) *pcShard {
	const m = uint64(11400714819323198485)
	h := (pageID * m) >> 32
	return &c.shards[h*uint64(c.numShard)>>32] //nolint:gosec // G115: h<2^32 and numShard<=16; product fits uint64
}

// get returns a copy of the cached page bytes for pageID, or nil on miss.
// On hit the reference bit is set.
func (c *pageCache) get(pageID uint64) []byte {
	s := c.shardFor(pageID)
	s.mu.Lock()
	e, ok := s.index[pageID]
	if !ok || e.ptype == pcEntryTest {
		s.mu.Unlock()
		c.misses.Add(1)
		return nil
	}
	e.referenced = true
	// return a copy so the caller owns it; the cache keeps its own copy
	out := make([]byte, len(e.data))
	copy(out, e.data)
	s.mu.Unlock()
	c.hits.Add(1)
	return out
}

// set stores a copy of data for pageID. If the page is already cached its
// value is replaced. If it was previously a ghost entry it is promoted to hot.
func (c *pageCache) set(pageID uint64, data []byte) {
	if len(data) == 0 {
		return
	}
	s := c.shardFor(pageID)
	s.mu.Lock()
	s.set(pageID, data)
	s.mu.Unlock()
}

// invalidate removes pageID from the cache entirely (all states). Called by
// the writer before releasing the write lock.
func (c *pageCache) invalidate(pageID uint64) {
	s := c.shardFor(pageID)
	s.mu.Lock()
	s.del(pageID)
	s.mu.Unlock()
}

// PageCacheStats returns cumulative hit and miss counts.
func (c *pageCache) stats() (hits, misses int64) {
	return c.hits.Load(), c.misses.Load()
}

// --- shard methods (all called with s.mu held) ---

func (s *pcShard) set(pageID uint64, data []byte) {
	cp := make([]byte, len(data))
	copy(cp, data)
	size := int64(len(cp))

	e, exists := s.index[pageID]
	switch {
	case !exists:
		// New cold entry.
		e = &pcEntry{pageID: pageID, data: cp, size: size, ptype: pcEntryCold}
		s.index[pageID] = e
		if s.handCold == nil {
			// Ring is empty — self-loop; set all hands.
			s.linkBefore(e, nil)
			s.handHot = e
			s.handCold = e
			s.handTest = e
		} else {
			s.linkBefore(e, s.handCold)
		}
		s.sizeCold += size
		s.countCold++
		s.evict()

	case e.ptype == pcEntryTest:
		// Ghost hit: remove test entry, re-insert as hot, widen coldTarget.
		s.sizeTest -= e.size
		s.countTest--
		s.coldTarget += size
		if s.coldTarget > s.maxSize {
			s.coldTarget = s.maxSize
		}
		// unlinkAdvance advances any hand (hot/cold/test) that pointed at e.
		// This prevents dangling hand pointers when e is re-inserted below.
		// After this call e.next == nil and e.prev == nil.
		s.unlinkAdvance(e)
		e.data = cp
		e.size = size
		e.ptype = pcEntryHot
		e.referenced = true
		// Re-insert: ring may now be empty (e was the sole element).
		if s.handHot == nil {
			s.linkBefore(e, nil)
			s.handHot = e
			s.handCold = e
			s.handTest = e
		} else {
			s.linkBefore(e, s.handHot)
		}
		s.sizeHot += size
		s.countHot++
		s.evict()

	default:
		// Update existing hot or cold entry.
		old := e.size
		e.data = cp
		e.size = size
		e.referenced = true
		switch e.ptype {
		case pcEntryHot:
			s.sizeHot += size - old
		case pcEntryCold:
			s.sizeCold += size - old
		}
		s.evict()
	}
}

func (s *pcShard) del(pageID uint64) {
	e, ok := s.index[pageID]
	if !ok {
		return
	}
	switch e.ptype {
	case pcEntryHot:
		s.sizeHot -= e.size
		s.countHot--
	case pcEntryCold:
		s.sizeCold -= e.size
		s.countCold--
	case pcEntryTest:
		s.sizeTest -= e.size
		s.countTest--
	}
	s.unlinkAdvance(e)
	delete(s.index, pageID)
}

func (s *pcShard) targetSize() int64 {
	if s.maxSize < 1 {
		return 1
	}
	return s.maxSize
}

// evict runs the CLOCK-Pro clock hands until hot+cold fits within maxSize.
func (s *pcShard) evict() {
	for s.sizeHot+s.sizeCold > s.targetSize() && s.handCold != nil {
		s.runHandCold()
	}
}

// runHandCold advances handCold, demoting cold→test or promoting cold→hot.
func (s *pcShard) runHandCold() {
	e := s.handCold
	if e == nil {
		return
	}
	// Check sole-element before any mutation.
	isSole := e.next == e

	if e.ptype == pcEntryCold {
		if e.referenced {
			// Promote to hot.
			e.referenced = false
			e.ptype = pcEntryHot
			s.sizeCold -= e.size
			s.countCold--
			s.sizeHot += e.size
			s.countHot++
		} else {
			// Evict data, demote to test (ghost).
			e.data = nil
			e.ptype = pcEntryTest
			s.sizeCold -= e.size
			s.countCold--
			s.sizeTest += e.size
			s.countTest++
			// Prune test list if too large.
			// runHandTest uses unlinkAdvance which will clear all hands if sole.
			for s.sizeTest > s.targetSize() && s.handTest != nil {
				s.runHandTest()
			}
			if s.handCold == nil {
				// runHandTest cleared all hands (sole element removed).
				return
			}
		}
	}

	if isSole {
		// Was sole — ring is now either empty or still has the demoted entry.
		// If it was demoted to test and then removed by runHandTest above,
		// handCold is already nil (handled above). If still present, sole:
		// no advance possible.
		return
	}
	s.handCold = s.handCold.next

	// If hot set is too large relative to coldTarget, cool one hot entry.
	for s.targetSize()-s.coldTarget < s.sizeHot && s.handHot != nil {
		s.runHandHot()
	}
}

// runHandHot advances handHot, cooling hot→cold if unreferenced.
func (s *pcShard) runHandHot() {
	if s.handHot == s.handTest && s.handTest != nil {
		s.runHandTest()
		if s.handHot == nil {
			return
		}
	}
	e := s.handHot
	if e.ptype == pcEntryHot {
		if e.referenced {
			e.referenced = false
		} else {
			e.ptype = pcEntryCold
			s.sizeHot -= e.size
			s.countHot--
			s.sizeCold += e.size
			s.countCold++
		}
	}
	if s.handHot.next == s.handHot {
		return
	}
	s.handHot = s.handHot.next
}

// runHandTest advances handTest, removing ghost entries and shrinking coldTarget.
func (s *pcShard) runHandTest() {
	e := s.handTest
	if e == nil {
		return
	}
	if e.ptype == pcEntryTest {
		s.sizeTest -= e.size
		s.countTest--
		s.coldTarget -= e.size
		if s.coldTarget < 0 {
			s.coldTarget = 0
		}
		// unlinkAdvance advances handTest (and handHot/handCold if they pointed here).
		// It also handles the sole-element case by clearing all hands.
		next := e.next
		if next == e {
			// Sole element — clear all hands before unlink.
			s.handHot = nil
			s.handCold = nil
			s.handTest = nil
			s.unlink(e)
		} else {
			s.unlinkAdvance(e)
			if s.handTest == e {
				s.handTest = next
			}
		}
		delete(s.index, e.pageID)
		return
	}
	if s.handTest.next == s.handTest {
		return
	}
	s.handTest = s.handTest.next
}

// --- ring list helpers ---

// linkBefore inserts e immediately before ref in the circular ring. If ref is
// nil the ring is empty and e points to itself.
func (s *pcShard) linkBefore(e, ref *pcEntry) {
	if ref == nil {
		e.next = e
		e.prev = e
		return
	}
	e.next = ref
	e.prev = ref.prev
	ref.prev.next = e
	ref.prev = e
}

// unlink removes e from the ring without advancing any hands.
func (s *pcShard) unlink(e *pcEntry) {
	if e.next == e {
		// sole element
		e.next = nil
		e.prev = nil
		return
	}
	e.prev.next = e.next
	e.next.prev = e.prev
	e.next = nil
	e.prev = nil
}

// unlinkAdvance removes e from the ring and advances any hands that pointed at e.
func (s *pcShard) unlinkAdvance(e *pcEntry) {
	if e.next == e || e.next == nil {
		// sole element — clear all hands
		s.handHot = nil
		s.handCold = nil
		s.handTest = nil
		e.next = nil
		e.prev = nil
		return
	}
	next := e.next
	if s.handHot == e {
		s.handHot = next
	}
	if s.handCold == e {
		s.handCold = next
	}
	if s.handTest == e {
		s.handTest = next
	}
	s.unlink(e)
}
