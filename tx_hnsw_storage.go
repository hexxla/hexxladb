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

	meta        *hnsw.Meta
	metaLoaded  bool
	metaFound   bool
	entry       lattice.PackedCoord
	entryLoaded bool
	entryFound  bool
	nodes       map[lattice.PackedCoord]cachedHNSWNode
	vectors     map[lattice.PackedCoord]cachedEmbedding
}

type cachedHNSWNode struct {
	node  *hnsw.Node
	found bool
}

type cachedEmbedding struct {
	vector []float32
	found  bool
}

func (s *txHNSWStorage) GetHNSWMeta() (*hnsw.Meta, bool, error) {
	if s.metaLoaded {
		return s.meta, s.metaFound, nil
	}
	val, ok, err := s.tx.getDirect([]byte(index.HNSWMetaKey))
	if err != nil || !ok {
		return nil, false, err
	}
	m, err := hnsw.DecodeMeta(val)
	if err != nil {
		return nil, false, err
	}
	s.meta = m
	s.metaLoaded = true
	s.metaFound = true
	return m, true, nil
}

func (s *txHNSWStorage) PutHNSWMeta(m *hnsw.Meta) error {
	if err := s.tx.putDirect([]byte(index.HNSWMetaKey), hnsw.EncodeMeta(m)); err != nil {
		return err
	}
	s.meta = m
	s.metaLoaded = true
	s.metaFound = true
	return nil
}

func (s *txHNSWStorage) GetHNSWEntry() (lattice.PackedCoord, bool, error) {
	if s.entryLoaded {
		return s.entry, s.entryFound, nil
	}
	val, ok, err := s.tx.getDirect([]byte(index.HNSWEntryKey))
	if err != nil || !ok {
		return lattice.PackedCoord{}, false, err
	}
	if len(val) < 16 {
		s.entryLoaded = true
		return lattice.PackedCoord{}, false, nil
	}
	var p lattice.PackedCoord
	p[1] = binary.BigEndian.Uint64(val[0:8])
	p[0] = binary.BigEndian.Uint64(val[8:16])
	s.entry = p
	s.entryLoaded = true
	s.entryFound = true
	return p, true, nil
}

func (s *txHNSWStorage) PutHNSWEntry(p lattice.PackedCoord) error {
	var buf [16]byte
	binary.BigEndian.PutUint64(buf[0:8], p[1])
	binary.BigEndian.PutUint64(buf[8:16], p[0])
	if err := s.tx.putDirect([]byte(index.HNSWEntryKey), buf[:]); err != nil {
		return err
	}
	s.entry = p
	s.entryLoaded = true
	s.entryFound = true
	return nil
}

func (s *txHNSWStorage) DeleteHNSWEntry() error {
	if err := s.tx.deleteDirect([]byte(index.HNSWEntryKey)); err != nil {
		return err
	}
	s.entry = lattice.PackedCoord{}
	s.entryLoaded = true
	s.entryFound = false
	return nil
}

func (s *txHNSWStorage) GetHNSWNode(p lattice.PackedCoord) (*hnsw.Node, bool, error) {
	if cached, ok := s.nodes[p]; ok {
		return cached.node, cached.found, nil
	}
	key := index.HNSWNodeKey(p)
	val, ok, err := s.tx.getDirect(key)
	if err != nil || !ok {
		return nil, false, err
	}
	n, err := hnsw.DecodeNode(p, val)
	if err != nil {
		return nil, false, err
	}
	if s.nodes == nil {
		s.nodes = make(map[lattice.PackedCoord]cachedHNSWNode)
	}
	s.nodes[p] = cachedHNSWNode{node: n, found: true}
	return n, true, nil
}

func (s *txHNSWStorage) PutHNSWNode(n *hnsw.Node) error {
	key := index.HNSWNodeKey(n.Coord)
	if err := s.tx.putDirect(key, hnsw.EncodeNode(n)); err != nil {
		return err
	}
	if s.nodes == nil {
		s.nodes = make(map[lattice.PackedCoord]cachedHNSWNode)
	}
	s.nodes[n.Coord] = cachedHNSWNode{node: n, found: true}
	return nil
}

func (s *txHNSWStorage) DeleteHNSWNode(p lattice.PackedCoord) error {
	if err := s.tx.deleteDirect(index.HNSWNodeKey(p)); err != nil {
		return err
	}
	if s.nodes != nil {
		s.nodes[p] = cachedHNSWNode{}
	}
	return nil
}

func (s *txHNSWStorage) GetEmbeddingVec(p lattice.PackedCoord) (vec []float32, found bool, err error) {
	if cached, ok := s.vectors[p]; ok {
		return cached.vector, cached.found, nil
	}
	key := index.EmbedKey(p)
	var val []byte
	val, found, err = s.tx.getDirect(key)
	if err != nil || !found {
		return nil, false, err
	}
	vector := decodeFloat32s(val)
	if s.vectors == nil {
		s.vectors = make(map[lattice.PackedCoord]cachedEmbedding)
	}
	s.vectors[p] = cachedEmbedding{vector: vector, found: true}
	return vector, true, nil
}
