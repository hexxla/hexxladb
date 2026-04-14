package record

import (
	"encoding/binary"
	"fmt"

	"github.com/hexxla/hexxladb/internal/lattice"
)

const maxTags = 65536

// EncodeCell encodes a cell record (envelope + v1 payload).
func EncodeCell(r CellRecord) ([]byte, error) {
	payload, err := encodeCellPayloadV1(r)
	if err != nil {
		return nil, err
	}
	return AppendEnvelope(nil, MagicCell, FormatVersionV1, payload)
}

// DecodeCell decodes a full cell record blob.
func DecodeCell(data []byte) (CellRecord, error) {
	_, payload, err := ParseEnvelope(MagicCell, data)
	if err != nil {
		return CellRecord{}, err
	}
	return decodeCellPayloadV1(payload)
}

func encodeCellPayloadV1(r CellRecord) ([]byte, error) {
	if len(r.Tags) > maxTags {
		return nil, fmt.Errorf("%w: too many tags", ErrInvalidRecord)
	}
	dst := appendPackedCoord(nil, r.Key)
	var err error
	dst, err = appendString32(dst, r.RawContent)
	if err != nil {
		return nil, err
	}
	dst, err = appendProvenance(dst, r.Provenance)
	if err != nil {
		return nil, err
	}
	dst, err = appendValidity(dst, r.Validity)
	if err != nil {
		return nil, err
	}
	var ntags [4]byte
	if err := putUint32BE(ntags[:], len(r.Tags)); err != nil {
		return nil, err
	}
	dst = append(dst, ntags[:]...)
	for _, t := range r.Tags {
		dst, err = appendString32(dst, t)
		if err != nil {
			return nil, err
		}
	}
	if r.ClusterHint == nil {
		dst = append(dst, 0)
	} else {
		dst = append(dst, 1)
		dst = appendPackedCoord(dst, *r.ClusterHint)
	}
	return dst, nil
}

func decodeCellPayloadV1(data []byte) (CellRecord, error) {
	var r CellRecord
	var err error
	r.Key, data, err = readPackedCoord(data)
	if err != nil {
		return CellRecord{}, err
	}
	r.RawContent, data, err = readString32(data)
	if err != nil {
		return CellRecord{}, err
	}
	r.Provenance, data, err = readProvenance(data)
	if err != nil {
		return CellRecord{}, err
	}
	r.Validity, data, err = readValidity(data)
	if err != nil {
		return CellRecord{}, err
	}
	if len(data) < 4 {
		return CellRecord{}, fmt.Errorf("%w: tag count", ErrInvalidRecord)
	}
	nt := binary.BigEndian.Uint32(data[0:4])
	data = data[4:]
	if nt > maxTags {
		return CellRecord{}, fmt.Errorf("%w: too many tags", ErrInvalidRecord)
	}
	r.Tags = make([]string, 0, nt)
	for range nt {
		var s string
		s, data, err = readString32(data)
		if err != nil {
			return CellRecord{}, err
		}
		r.Tags = append(r.Tags, s)
	}
	if len(data) < 1 {
		return CellRecord{}, fmt.Errorf("%w: cluster hint", ErrInvalidRecord)
	}
	switch data[0] {
	case 0:
		data = data[1:]
	case 1:
		data = data[1:]
		var hint lattice.PackedCoord
		hint, data, err = readPackedCoord(data)
		if err != nil {
			return CellRecord{}, err
		}
		r.ClusterHint = &hint
	default:
		return CellRecord{}, fmt.Errorf("%w: cluster hint flag", ErrInvalidRecord)
	}
	if len(data) != 0 {
		return CellRecord{}, fmt.Errorf("%w: trailing cell bytes", ErrInvalidRecord)
	}
	return r, nil
}

func appendProvenance(dst []byte, p ProvenanceWire) ([]byte, error) {
	var err error
	dst, err = appendString32(dst, p.SourceID)
	if err != nil {
		return nil, err
	}
	dst = appendFloat64BE(dst, p.Confidence)
	dst = appendInt64BE(dst, p.CreatedAt)
	dst = appendInt64BE(dst, p.UpdatedAt)
	return dst, nil
}

func readProvenance(data []byte) (ProvenanceWire, []byte, error) {
	var p ProvenanceWire
	var err error
	p.SourceID, data, err = readString32(data)
	if err != nil {
		return ProvenanceWire{}, nil, err
	}
	p.Confidence, data, err = readFloat64BE(data)
	if err != nil {
		return ProvenanceWire{}, nil, err
	}
	p.CreatedAt, data, err = readInt64BE(data)
	if err != nil {
		return ProvenanceWire{}, nil, err
	}
	p.UpdatedAt, data, err = readInt64BE(data)
	if err != nil {
		return ProvenanceWire{}, nil, err
	}
	return p, data, nil
}

func appendValidity(dst []byte, v ValidityWire) ([]byte, error) {
	dst = appendOptionalInt64(dst, v.ValidFrom)
	dst = appendOptionalInt64(dst, v.ValidTo)
	return dst, nil
}

func appendOptionalInt64(dst []byte, v *int64) []byte {
	if v == nil {
		return append(dst, 0)
	}
	dst = append(dst, 1)
	return appendInt64BE(dst, *v)
}

func readValidity(data []byte) (ValidityWire, []byte, error) {
	var v ValidityWire
	var err error
	v.ValidFrom, data, err = readOptionalInt64(data)
	if err != nil {
		return ValidityWire{}, nil, err
	}
	v.ValidTo, data, err = readOptionalInt64(data)
	if err != nil {
		return ValidityWire{}, nil, err
	}
	return v, data, nil
}

func readOptionalInt64(data []byte) (v *int64, rest []byte, err error) {
	if len(data) < 1 {
		return nil, nil, fmt.Errorf("%w: optional int64", ErrInvalidRecord)
	}
	switch data[0] {
	case 0:
		return nil, data[1:], nil
	case 1:
		if len(data) < 1+8 {
			return nil, nil, fmt.Errorf("%w: optional int64 value", ErrInvalidRecord)
		}
		x, rest, err := readInt64BE(data[1:])
		if err != nil {
			return nil, nil, err
		}
		v = &x
		return v, rest, nil
	default:
		return nil, nil, fmt.Errorf("%w: optional int64 flag", ErrInvalidRecord)
	}
}
