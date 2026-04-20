package record

import (
	"errors"
	"fmt"

	"github.com/oklog/ulid/v2"

	"github.com/hexxla/hexxladb/internal/lattice"
)

// EncodeSeam encodes a seam record (envelope + v1 payload).
func EncodeSeam(r SeamRecord) ([]byte, error) {
	id, err := ulid.Parse(r.ID)
	if err != nil {
		return nil, errors.Join(ErrInvalidULID, err)
	}
	payload, err := encodeSeamPayloadV1(r, id)
	if err != nil {
		return nil, err
	}
	return AppendEnvelope(nil, MagicSeam, FormatVersionV1, payload)
}

// DecodeSeam decodes a full seam record blob.
func DecodeSeam(data []byte) (SeamRecord, error) {
	_, payload, err := ParseEnvelope(MagicSeam, data)
	if err != nil {
		return SeamRecord{}, err
	}
	return decodeSeamPayloadV1(payload)
}

func encodeSeamPayloadV1(r SeamRecord, id ulid.ULID) ([]byte, error) {
	dst := append([]byte(nil), id[:]...)
	dst = appendPackedCoord(dst, r.CellA)
	dst = appendPackedCoord(dst, r.CellB)
	var err error
	dst, err = appendString32(dst, r.SeamType)
	if err != nil {
		return nil, err
	}
	dst, err = appendString32(dst, r.Reason)
	if err != nil {
		return nil, err
	}
	dst = appendFloat64BE(dst, r.ConfidenceDelta)
	dst = appendInt64BE(dst, r.DetectedAt)
	dst, err = appendString32(dst, r.ResolutionStatus)
	if err != nil {
		return nil, err
	}
	dst, err = appendString32(dst, r.ResolutionNote)
	if err != nil {
		return nil, err
	}
	dst, err = appendValidity(dst, r.Validity)
	if err != nil {
		return nil, err
	}
	dst, err = appendProvenance(dst, r.Provenance)
	if err != nil {
		return nil, err
	}
	return dst, nil
}

func decodeSeamPayloadV1(data []byte) (SeamRecord, error) {
	var r SeamRecord
	if len(data) < 16 {
		return SeamRecord{}, fmt.Errorf("%w: ulid", ErrInvalidRecord)
	}
	var id ulid.ULID
	copy(id[:], data[0:16])
	data = data[16:]
	if _, err := ulid.Parse(id.String()); err != nil {
		return SeamRecord{}, errors.Join(ErrInvalidULID, err)
	}
	r.ID = id.String()
	var err error
	r.CellA, data, err = readPackedCoord(data)
	if err != nil {
		return SeamRecord{}, err
	}
	r.CellB, data, err = readPackedCoord(data)
	if err != nil {
		return SeamRecord{}, err
	}
	r.SeamType, data, err = readString32(data)
	if err != nil {
		return SeamRecord{}, err
	}
	r.Reason, data, err = readString32(data)
	if err != nil {
		return SeamRecord{}, err
	}
	r.ConfidenceDelta, data, err = readFloat64BE(data)
	if err != nil {
		return SeamRecord{}, err
	}
	r.DetectedAt, data, err = readInt64BE(data)
	if err != nil {
		return SeamRecord{}, err
	}
	r.ResolutionStatus, data, err = readString32(data)
	if err != nil {
		return SeamRecord{}, err
	}
	r.ResolutionNote, data, err = readString32(data)
	if err != nil {
		return SeamRecord{}, err
	}
	if len(data) == 0 {
		return r, nil
	}
	r.Validity, data, err = readValidity(data)
	if err != nil {
		return SeamRecord{}, err
	}
	if len(data) == 0 {
		return r, nil
	}
	r.Provenance, data, err = readProvenance(data)
	if err != nil {
		return SeamRecord{}, err
	}
	if len(data) != 0 {
		return SeamRecord{}, fmt.Errorf("%w: trailing seam bytes", ErrInvalidRecord)
	}
	return r, nil
}

// CanonicalCellPair returns (min,max) PackedCoord for seam-by-cells index ordering.
func CanonicalCellPair(a, b lattice.PackedCoord) (lo, hi lattice.PackedCoord) {
	if a.Compare(b) <= 0 {
		return a, b
	}
	return b, a
}
