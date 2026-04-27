package hexxladb

import (
	"encoding/binary"

	"github.com/hexxla/hexxladb/internal/hnsw"
	"github.com/hexxla/hexxladb/internal/index"
	"github.com/hexxla/hexxladb/internal/lattice"
)

// txHNSWStorage adapts a Tx to the hnsw.Storage interface.
type txHNSWStorage struct {
	tx *Tx
}

func (s *txHNSWStorage) GetHNSWMeta() (*hnsw.Meta, bool, error) {
	val, ok, err := s.tx.getDirect([]byte(index.HNSWMetaKey))
	if err != nil || !ok {
		return nil, false, err
	}
	m, err := hnsw.DecodeMeta(val)
	if err != nil {
		return nil, false, err
	}
	return m, true, nil
}

func (s *txHNSWStorage) PutHNSWMeta(m *hnsw.Meta) error {
	return s.tx.putDirect([]byte(index.HNSWMetaKey), hnsw.EncodeMeta(m))
}

func (s *txHNSWStorage) GetHNSWEntry() (lattice.PackedCoord, bool, error) {
	val, ok, err := s.tx.getDirect([]byte(index.HNSWEntryKey))
	if err != nil || !ok {
		return lattice.PackedCoord{}, false, err
	}
	if len(val) < 16 {
		return lattice.PackedCoord{}, false, nil
	}
	var p lattice.PackedCoord
	p[1] = binary.BigEndian.Uint64(val[0:8])
	p[0] = binary.BigEndian.Uint64(val[8:16])
	return p, true, nil
}

func (s *txHNSWStorage) PutHNSWEntry(p lattice.PackedCoord) error {
	var buf [16]byte
	binary.BigEndian.PutUint64(buf[0:8], p[1])
	binary.BigEndian.PutUint64(buf[8:16], p[0])
	return s.tx.putDirect([]byte(index.HNSWEntryKey), buf[:])
}

func (s *txHNSWStorage) DeleteHNSWEntry() error {
	return s.tx.deleteDirect([]byte(index.HNSWEntryKey))
}

func (s *txHNSWStorage) GetHNSWNode(p lattice.PackedCoord) (*hnsw.Node, bool, error) {
	key := index.HNSWNodeKey(p)
	val, ok, err := s.tx.getDirect(key)
	if err != nil || !ok {
		return nil, false, err
	}
	n, err := hnsw.DecodeNode(p, val)
	if err != nil {
		return nil, false, err
	}
	return n, true, nil
}

func (s *txHNSWStorage) PutHNSWNode(n *hnsw.Node) error {
	key := index.HNSWNodeKey(n.Coord)
	return s.tx.putDirect(key, hnsw.EncodeNode(n))
}

func (s *txHNSWStorage) DeleteHNSWNode(p lattice.PackedCoord) error {
	return s.tx.deleteDirect(index.HNSWNodeKey(p))
}

func (s *txHNSWStorage) GetEmbeddingVec(p lattice.PackedCoord) (vec []float32, found bool, err error) {
	key := index.EmbedKey(p)
	var val []byte
	val, found, err = s.tx.getDirect(key)
	if err != nil || !found {
		return nil, false, err
	}
	return decodeFloat32s(val), true, nil
}
