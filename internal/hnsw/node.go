// Package hnsw implements an HNSW (Hierarchical Navigable Small World) graph
// for approximate nearest-neighbor search. The graph is persisted in the
// hexxladb B+ tree under the hnsw/ keyspace.
package hnsw

import (
	"encoding/binary"
	"fmt"

	"github.com/hexxla/hexxladb/internal/lattice"
)

// Node is the in-memory representation of an HNSW graph node.
// Each node stores neighbor lists for layers 0..MaxLayer.
type Node struct {
	Coord    lattice.PackedCoord
	MaxLayer uint8
	// Neighbors[i] is the neighbor list at layer i (0-indexed).
	// len(Neighbors) == MaxLayer + 1.
	Neighbors [][]lattice.PackedCoord
}

// packedCoordSize is the byte length of a serialized PackedCoord.
const packedCoordSize = 16

// EncodeNode serializes a Node to bytes.
//
// Wire format:
//
//	[maxLayer uint8]
//	for each layer 0..maxLayer:
//	  [count uint16 big-endian]
//	  [count × PackedCoord (16 bytes each, Hi then Lo big-endian)]
func EncodeNode(n *Node) []byte {
	// Calculate size.
	size := 1 // maxLayer
	for _, layer := range n.Neighbors {
		size += 2 + len(layer)*packedCoordSize // count + coords
	}
	buf := make([]byte, 0, size)
	buf = append(buf, n.MaxLayer)
	for _, layer := range n.Neighbors {
		var cnt [2]byte
		binary.BigEndian.PutUint16(cnt[:], uint16(len(layer))) //nolint:gosec // bounded by M_max0
		buf = append(buf, cnt[:]...)
		for _, coord := range layer {
			var tmp [packedCoordSize]byte
			binary.BigEndian.PutUint64(tmp[0:8], coord[1])  // Hi
			binary.BigEndian.PutUint64(tmp[8:16], coord[0]) // Lo
			buf = append(buf, tmp[:]...)
		}
	}
	return buf
}

// DecodeNode deserializes a Node from bytes produced by [EncodeNode].
func DecodeNode(coord lattice.PackedCoord, data []byte) (*Node, error) {
	if len(data) < 1 {
		return nil, fmt.Errorf("hnsw: node data too short")
	}
	maxLayer := data[0]
	data = data[1:]
	layers := make([][]lattice.PackedCoord, int(maxLayer)+1)
	for i := range layers {
		if len(data) < 2 {
			return nil, fmt.Errorf("hnsw: truncated layer %d count", i)
		}
		count := int(binary.BigEndian.Uint16(data[:2]))
		data = data[2:]
		need := count * packedCoordSize
		if len(data) < need {
			return nil, fmt.Errorf("hnsw: truncated layer %d neighbors", i)
		}
		neighbors := make([]lattice.PackedCoord, count)
		for j := range count {
			off := j * packedCoordSize
			neighbors[j][1] = binary.BigEndian.Uint64(data[off : off+8])
			neighbors[j][0] = binary.BigEndian.Uint64(data[off+8 : off+16])
		}
		data = data[need:]
		layers[i] = neighbors
	}
	if len(data) != 0 {
		return nil, fmt.Errorf("hnsw: %d trailing bytes", len(data))
	}
	return &Node{
		Coord:     coord,
		MaxLayer:  maxLayer,
		Neighbors: layers,
	}, nil
}

// Meta holds the HNSW graph metadata persisted in hnsw/meta.
type Meta struct {
	M        uint16 // max bidirectional connections per layer ≥ 1
	EfC      uint16 // efConstruction — build-time beam width
	MaxLayer uint8  // current maximum layer in the graph
	Count    uint64 // number of nodes in the graph
}

// EncodeMeta serializes Meta.
//
// Wire format: [M uint16][EfC uint16][MaxLayer uint8][Count uint64] = 13 bytes.
func EncodeMeta(m *Meta) []byte {
	buf := make([]byte, 13)
	binary.BigEndian.PutUint16(buf[0:2], m.M)
	binary.BigEndian.PutUint16(buf[2:4], m.EfC)
	buf[4] = m.MaxLayer
	binary.BigEndian.PutUint64(buf[5:13], m.Count)
	return buf
}

// DecodeMeta deserializes Meta from bytes produced by [EncodeMeta].
func DecodeMeta(data []byte) (*Meta, error) {
	if len(data) < 13 {
		return nil, fmt.Errorf("hnsw: meta data too short (%d bytes)", len(data))
	}
	return &Meta{
		M:        binary.BigEndian.Uint16(data[0:2]),
		EfC:      binary.BigEndian.Uint16(data[2:4]),
		MaxLayer: data[4],
		Count:    binary.BigEndian.Uint64(data[5:13]),
	}, nil
}
