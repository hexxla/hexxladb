package record

import (
	"encoding/binary"
	"fmt"
	"math"

	"github.com/hexxla/hexxladb/internal/lattice"
)

func appendPackedCoord(dst []byte, p lattice.PackedCoord) []byte {
	var b [16]byte
	binary.BigEndian.PutUint64(b[0:8], p[0])
	binary.BigEndian.PutUint64(b[8:16], p[1])
	return append(dst, b[:]...)
}

func readPackedCoord(data []byte) (lattice.PackedCoord, []byte, error) {
	if len(data) < 16 {
		return lattice.PackedCoord{}, nil, fmt.Errorf("%w: packed coord", ErrInvalidRecord)
	}
	var p lattice.PackedCoord
	p[0] = binary.BigEndian.Uint64(data[0:8])
	p[1] = binary.BigEndian.Uint64(data[8:16])
	return p, data[16:], nil
}

func appendString32(dst []byte, s string) ([]byte, error) {
	if len(s) > MaxStringField {
		return dst, fmt.Errorf("%w: string field too long", ErrInvalidRecord)
	}
	var b [4]byte
	if err := putUint32BE(b[:], len(s)); err != nil {
		return dst, err
	}
	dst = append(dst, b[:]...)
	dst = append(dst, s...)
	return dst, nil
}

func readString32(data []byte) (s string, rest []byte, err error) {
	if len(data) < 4 {
		return "", nil, fmt.Errorf("%w: string length", ErrInvalidRecord)
	}
	n := binary.BigEndian.Uint32(data[0:4])
	if n > MaxStringField {
		return "", nil, fmt.Errorf("%w: string field too long", ErrInvalidRecord)
	}
	if uint64(len(data)) < 4+uint64(n) {
		return "", nil, fmt.Errorf("%w: string data", ErrInvalidRecord)
	}
	s = string(data[4 : 4+n])
	rest = data[4+n:]
	return s, rest, nil
}

func appendFloat64BE(dst []byte, v float64) []byte {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], math.Float64bits(v))
	return append(dst, b[:]...)
}

func readFloat64BE(data []byte) (v float64, rest []byte, err error) {
	if len(data) < 8 {
		return 0, nil, fmt.Errorf("%w: float64", ErrInvalidRecord)
	}
	v = math.Float64frombits(binary.BigEndian.Uint64(data[0:8]))
	rest = data[8:]
	return v, rest, nil
}
