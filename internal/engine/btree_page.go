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
	btreeVersion        = 1
	btreeKindLeaf       = 1
	btreeKindInternal   = 2
	btreeHeaderSize     = 64
	maxKeyBytes         = 256
	maxValBytes         = 512
	maxLeafEntries      = 32
	maxInternalChildren = 32 // max keys = maxInternalChildren - 1
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

func parseLeafPage(page []byte) (*leafData, error) {
	if len(page) != PageSize {
		return nil, fmt.Errorf("%w: bad page len", ErrCorruptTree)
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
	if n > maxLeafEntries {
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
		if kl > maxKeyBytes || vl > maxValBytes {
			return nil, fmt.Errorf("%w: leaf kv len", ErrCorruptTree)
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

func buildLeafPage(parent, next uint64, keys, vals [][]byte) ([]byte, error) {
	if len(keys) != len(vals) {
		return nil, fmt.Errorf("%w: leaf kv mismatch", ErrCorruptTree)
	}
	if len(keys) > maxLeafEntries {
		return nil, fmt.Errorf("%w: leaf overflow", ErrCorruptTree)
	}
	page := make([]byte, PageSize)
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
		if len(v) > maxValBytes {
			return nil, ErrValueTooLarge
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
	if len(page) != PageSize {
		return nil, fmt.Errorf("%w: bad page len", ErrCorruptTree)
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
	if n > maxInternalChildren-1 {
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

func buildInternalPage(parent uint64, ptrs []uint64, keys [][]byte) ([]byte, error) {
	if len(ptrs) != len(keys)+1 {
		return nil, fmt.Errorf("%w: internal ptr/key mismatch", ErrCorruptTree)
	}
	if len(keys) >= maxInternalChildren {
		return nil, fmt.Errorf("%w: internal overflow", ErrCorruptTree)
	}
	page := make([]byte, PageSize)
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
