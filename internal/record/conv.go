package record

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
)

// uint32FromInt returns n as uint32 after proving it fits (avoids unchecked int→uint32 narrowing).
func uint32FromInt(n int) (uint32, error) {
	if n < 0 {
		return 0, fmt.Errorf("%w: negative integer", ErrInvalidRecord)
	}
	if uint64(n) > uint64(math.MaxUint32) {
		return 0, fmt.Errorf("%w: value overflows uint32", ErrInvalidRecord)
	}
	return uint32(n), nil
}

func putUint32BE(dst []byte, n int) error {
	u, err := uint32FromInt(n)
	if err != nil {
		return err
	}
	binary.BigEndian.PutUint32(dst, u)
	return nil
}

// appendInt64BE encodes v with encoding/binary (fixed size; no manual uint64 reinterpretation in app code).
func appendInt64BE(dst []byte, v int64) []byte {
	var tmp [8]byte
	buf := bytes.NewBuffer(tmp[:0])
	_ = binary.Write(buf, binary.BigEndian, v)
	return append(dst, buf.Bytes()...)
}

func readInt64BE(data []byte) (v int64, rest []byte, err error) {
	if len(data) < 8 {
		return 0, nil, fmt.Errorf("%w: int64", ErrInvalidRecord)
	}
	r := bytes.NewReader(data[:8])
	if err := binary.Read(r, binary.BigEndian, &v); err != nil {
		return 0, nil, err
	}
	return v, data[8:], nil
}
