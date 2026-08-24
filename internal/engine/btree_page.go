package engine

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"sort"
)

func uint16FromInt(n int, ctx string) (uint16, error) {
	if n < 0 || n > 65535 {
		return 0, fmt.Errorf("%w: %s overflow", ErrCorruptTree, ctx)
	}
	v := uint16(n)
	return v, nil
}

const (
	btreeVersion      = 1
	btreeKindLeaf     = 1
	btreeKindInternal = 2
	btreeHeaderSize   = 64
	maxKeyBytes       = 256
	minKeysPerPage    = 2 // minimum entries to allow a split (bbolt pattern)
)

type leafData struct {
	next   uint64
	parent uint64
	keys   [][]byte
	vals   [][]byte
}

type internalData struct {
	parent uint64
	ptrs   []uint64
	keys   [][]byte
}

// maxLeafEntriesForPage returns the upper bound on leaf entries for the given page size.
func maxLeafEntriesForPage(pageSize int) uint16 {
	// Minimum entry: 4 (lengths) + 1 (key) + 0 (val) = 5 bytes.
	maxN := min((pageSize-btreeHeaderSize)/5, 65535)
	return uint16(maxN) //nolint:gosec // capped at 65535
}

// maxInternalKeysForPage returns the upper bound on internal keys for the given page size.
func maxInternalKeysForPage(pageSize int) uint16 {
	// Minimum entry: 2 (keyLen) + 1 (key) + 8 (ptr) = 11, plus leading ptr0 = 8.
	maxN := min((pageSize-btreeHeaderSize-8)/11, 65535)
	return uint16(maxN) //nolint:gosec // capped at 65535
}

func parseLeafPage(page []byte) (*leafData, error) {
	pageSize := len(page)
	if !IsValidPageSize(uint32(pageSize)) { //nolint:gosec // pageSize is always positive
		return nil, fmt.Errorf("%w: bad page len %d", ErrCorruptTree, pageSize)
	}
	if string(page[0:4]) != btreeNodeMagic {
		return nil, fmt.Errorf("%w: leaf magic", ErrCorruptTree)
	}
	if page[4] != btreeVersion {
		return nil, fmt.Errorf("%w: leaf version", ErrCorruptTree)
	}
	if page[5] != btreeKindLeaf {
		return nil, fmt.Errorf("%w: not leaf", ErrCorruptTree)
	}
	n := binary.BigEndian.Uint16(page[6:8])
	if n > maxLeafEntriesForPage(pageSize) {
		return nil, fmt.Errorf("%w: leaf nkeys", ErrCorruptTree)
	}
	next := binary.BigEndian.Uint64(page[8:16])
	parent := binary.BigEndian.Uint64(page[16:24])

	off := btreeHeaderSize
	keys := make([][]byte, 0, n)
	vals := make([][]byte, 0, n)
	for range n {
		if off+4 > len(page) {
			return nil, fmt.Errorf("%w: leaf truncated", ErrCorruptTree)
		}
		kl := binary.BigEndian.Uint16(page[off : off+2])
		vl := binary.BigEndian.Uint16(page[off+2 : off+4])
		off += 4
		if kl > maxKeyBytes {
			return nil, fmt.Errorf("%w: leaf key len", ErrCorruptTree)
		}
		if off+int(kl)+int(vl) > len(page) {
			return nil, fmt.Errorf("%w: leaf payload", ErrCorruptTree)
		}
		k := append([]byte(nil), page[off:off+int(kl)]...)
		off += int(kl)
		v := append([]byte(nil), page[off:off+int(vl)]...)
		off += int(vl)
		keys = append(keys, k)
		vals = append(vals, v)
	}
	return &leafData{next: next, parent: parent, keys: keys, vals: vals}, nil
}

// lookupLeafValue validates a serialized leaf and returns the matching
// page-backed value without materializing every key and value on the page.
// The caller must copy the result before releasing the page buffer.
func lookupLeafValue(page, key []byte) ([]byte, bool, error) {
	pageSize := len(page)
	if !IsValidPageSize(uint32(pageSize)) { //nolint:gosec // pageSize is always positive
		return nil, false, fmt.Errorf("%w: bad page len %d", ErrCorruptTree, pageSize)
	}
	if string(page[0:4]) != btreeNodeMagic {
		return nil, false, fmt.Errorf("%w: leaf magic", ErrCorruptTree)
	}
	if page[4] != btreeVersion {
		return nil, false, fmt.Errorf("%w: leaf version", ErrCorruptTree)
	}
	if page[5] != btreeKindLeaf {
		return nil, false, fmt.Errorf("%w: not leaf", ErrCorruptTree)
	}
	n := binary.BigEndian.Uint16(page[6:8])
	if n > maxLeafEntriesForPage(pageSize) {
		return nil, false, fmt.Errorf("%w: leaf nkeys", ErrCorruptTree)
	}

	var found []byte
	off := btreeHeaderSize
	for range n {
		if off+4 > len(page) {
			return nil, false, fmt.Errorf("%w: leaf truncated", ErrCorruptTree)
		}
		keyLen := int(binary.BigEndian.Uint16(page[off : off+2]))
		valueLen := int(binary.BigEndian.Uint16(page[off+2 : off+4]))
		off += 4
		if keyLen > maxKeyBytes {
			return nil, false, fmt.Errorf("%w: leaf key len", ErrCorruptTree)
		}
		if off+keyLen+valueLen > len(page) {
			return nil, false, fmt.Errorf("%w: leaf payload", ErrCorruptTree)
		}
		storedKey := page[off : off+keyLen]
		off += keyLen
		value := page[off : off+valueLen]
		off += valueLen
		if bytes.Equal(storedKey, key) {
			found = value
		}
	}
	return found, found != nil, nil
}

func buildLeafPage(pageSize int, parent, next uint64, keys, vals [][]byte) ([]byte, error) {
	if len(keys) != len(vals) {
		return nil, fmt.Errorf("%w: leaf kv mismatch", ErrCorruptTree)
	}
	page := make([]byte, pageSize)
	copy(page[0:4], btreeNodeMagic)
	page[4] = btreeVersion
	page[5] = btreeKindLeaf
	nk, err := uint16FromInt(len(keys), "leaf nkeys")
	if err != nil {
		return nil, err
	}
	binary.BigEndian.PutUint16(page[6:8], nk)
	binary.BigEndian.PutUint64(page[8:16], next)
	binary.BigEndian.PutUint64(page[16:24], parent)
	off := btreeHeaderSize
	for i := range keys {
		k, v := keys[i], vals[i]
		if len(k) > maxKeyBytes {
			return nil, ErrKeyTooLarge
		}
		if off+4+len(k)+len(v) > len(page) {
			return nil, fmt.Errorf("%w: leaf page full", ErrCorruptTree)
		}
		kl, err := uint16FromInt(len(k), "key len")
		if err != nil {
			return nil, err
		}
		vl, err := uint16FromInt(len(v), "val len")
		if err != nil {
			return nil, err
		}
		binary.BigEndian.PutUint16(page[off:off+2], kl)
		binary.BigEndian.PutUint16(page[off+2:off+4], vl)
		off += 4
		copy(page[off:], k)
		off += len(k)
		copy(page[off:], v)
		off += len(v)
	}
	return page, nil
}

func parseInternalPage(page []byte) (*internalData, error) {
	pageSize := len(page)
	if !IsValidPageSize(uint32(pageSize)) { //nolint:gosec // pageSize is always positive
		return nil, fmt.Errorf("%w: bad page len %d", ErrCorruptTree, pageSize)
	}
	if string(page[0:4]) != btreeNodeMagic {
		return nil, fmt.Errorf("%w: internal magic", ErrCorruptTree)
	}
	if page[4] != btreeVersion {
		return nil, fmt.Errorf("%w: internal version", ErrCorruptTree)
	}
	if page[5] != btreeKindInternal {
		return nil, fmt.Errorf("%w: not internal", ErrCorruptTree)
	}
	n := binary.BigEndian.Uint16(page[6:8])
	if n > maxInternalKeysForPage(pageSize) {
		return nil, fmt.Errorf("%w: internal nkeys", ErrCorruptTree)
	}
	parent := binary.BigEndian.Uint64(page[16:24])
	off := btreeHeaderSize
	if off+8 > len(page) {
		return nil, fmt.Errorf("%w: internal ptr0", ErrCorruptTree)
	}
	ptr0 := binary.BigEndian.Uint64(page[off : off+8])
	off += 8
	keys := make([][]byte, 0, n)
	ptrs := []uint64{ptr0}
	for range n {
		if off+2 > len(page) {
			return nil, fmt.Errorf("%w: internal key len", ErrCorruptTree)
		}
		kl := binary.BigEndian.Uint16(page[off : off+2])
		off += 2
		if kl > maxKeyBytes {
			return nil, fmt.Errorf("%w: internal key", ErrCorruptTree)
		}
		if off+int(kl)+8 > len(page) {
			return nil, fmt.Errorf("%w: internal key data", ErrCorruptTree)
		}
		k := append([]byte(nil), page[off:off+int(kl)]...)
		off += int(kl)
		p := binary.BigEndian.Uint64(page[off : off+8])
		off += 8
		keys = append(keys, k)
		ptrs = append(ptrs, p)
	}
	return &internalData{parent: parent, ptrs: ptrs, keys: keys}, nil
}

// lookupInternalChild validates a serialized internal page and returns the
// child selected for key without allocating decoded key or pointer slices.
func lookupInternalChild(page, key []byte) (uint64, error) {
	pageSize := len(page)
	if !IsValidPageSize(uint32(pageSize)) { //nolint:gosec // pageSize is always positive
		return 0, fmt.Errorf("%w: bad page len %d", ErrCorruptTree, pageSize)
	}
	if string(page[0:4]) != btreeNodeMagic {
		return 0, fmt.Errorf("%w: internal magic", ErrCorruptTree)
	}
	if page[4] != btreeVersion {
		return 0, fmt.Errorf("%w: internal version", ErrCorruptTree)
	}
	if page[5] != btreeKindInternal {
		return 0, fmt.Errorf("%w: not internal", ErrCorruptTree)
	}
	n := binary.BigEndian.Uint16(page[6:8])
	if n > maxInternalKeysForPage(pageSize) {
		return 0, fmt.Errorf("%w: internal nkeys", ErrCorruptTree)
	}
	off := btreeHeaderSize
	if off+8 > len(page) {
		return 0, fmt.Errorf("%w: internal ptr0", ErrCorruptTree)
	}
	child := binary.BigEndian.Uint64(page[off : off+8])
	off += 8
	chooseRight := true
	for range n {
		if off+2 > len(page) {
			return 0, fmt.Errorf("%w: internal key len", ErrCorruptTree)
		}
		keyLen := int(binary.BigEndian.Uint16(page[off : off+2]))
		off += 2
		if keyLen > maxKeyBytes {
			return 0, fmt.Errorf("%w: internal key", ErrCorruptTree)
		}
		if off+keyLen+8 > len(page) {
			return 0, fmt.Errorf("%w: internal key data", ErrCorruptTree)
		}
		separator := page[off : off+keyLen]
		off += keyLen
		pointer := binary.BigEndian.Uint64(page[off : off+8])
		off += 8
		if chooseRight && bytes.Compare(key, separator) >= 0 {
			child = pointer
		} else {
			chooseRight = false
		}
	}
	return child, nil
}

func buildInternalPage(pageSize int, parent uint64, ptrs []uint64, keys [][]byte) ([]byte, error) {
	if len(ptrs) != len(keys)+1 {
		return nil, fmt.Errorf("%w: internal ptr/key mismatch", ErrCorruptTree)
	}
	page := make([]byte, pageSize)
	copy(page[0:4], btreeNodeMagic)
	page[4] = btreeVersion
	page[5] = btreeKindInternal
	nk, err := uint16FromInt(len(keys), "internal nkeys")
	if err != nil {
		return nil, err
	}
	binary.BigEndian.PutUint16(page[6:8], nk)
	binary.BigEndian.PutUint64(page[8:16], 0)
	binary.BigEndian.PutUint64(page[16:24], parent)
	off := btreeHeaderSize
	binary.BigEndian.PutUint64(page[off:off+8], ptrs[0])
	off += 8
	for i := range keys {
		k := keys[i]
		if len(k) > maxKeyBytes {
			return nil, ErrKeyTooLarge
		}
		if off+2+len(k)+8 > len(page) {
			return nil, fmt.Errorf("%w: internal page full", ErrCorruptTree)
		}
		kl, err := uint16FromInt(len(k), "internal key len")
		if err != nil {
			return nil, err
		}
		binary.BigEndian.PutUint16(page[off:off+2], kl)
		off += 2
		copy(page[off:], k)
		off += len(k)
		binary.BigEndian.PutUint64(page[off:off+8], ptrs[i+1])
		off += 8
	}
	return page, nil
}

// leafKeyIndex returns the first index i with keys[i] >= key (by bytes.Compare).
func leafKeyIndex(keys [][]byte, key []byte) int {
	return sort.Search(len(keys), func(i int) bool {
		return bytes.Compare(keys[i], key) >= 0
	})
}

// internalPickChild returns the child index for key (into ptrs).
func internalPickChild(keys [][]byte, key []byte) int {
	return sort.Search(len(keys), func(j int) bool {
		return bytes.Compare(key, keys[j]) < 0
	})
}
