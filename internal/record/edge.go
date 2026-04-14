package record

import "fmt"

// EncodeEdge encodes an edge record (envelope + v1 payload).
func EncodeEdge(r EdgeRecord) ([]byte, error) {
	payload, err := encodeEdgePayloadV1(r)
	if err != nil {
		return nil, err
	}
	return AppendEnvelope(nil, MagicEdge, FormatVersionV1, payload)
}

// DecodeEdge decodes a full edge record blob.
func DecodeEdge(data []byte) (EdgeRecord, error) {
	_, payload, err := ParseEnvelope(MagicEdge, data)
	if err != nil {
		return EdgeRecord{}, err
	}
	return decodeEdgePayloadV1(payload)
}

func encodeEdgePayloadV1(r EdgeRecord) ([]byte, error) {
	dst := appendPackedCoord(nil, r.From)
	dst = appendPackedCoord(dst, r.To)
	var err error
	dst, err = appendString32(dst, r.RelationType)
	if err != nil {
		return nil, err
	}
	dst = appendFloat64BE(dst, r.Weight)
	dst, err = appendProvenance(dst, r.Provenance)
	if err != nil {
		return nil, err
	}
	return dst, nil
}

func decodeEdgePayloadV1(data []byte) (EdgeRecord, error) {
	var r EdgeRecord
	var err error
	r.From, data, err = readPackedCoord(data)
	if err != nil {
		return EdgeRecord{}, err
	}
	r.To, data, err = readPackedCoord(data)
	if err != nil {
		return EdgeRecord{}, err
	}
	r.RelationType, data, err = readString32(data)
	if err != nil {
		return EdgeRecord{}, err
	}
	r.Weight, data, err = readFloat64BE(data)
	if err != nil {
		return EdgeRecord{}, err
	}
	r.Provenance, data, err = readProvenance(data)
	if err != nil {
		return EdgeRecord{}, err
	}
	if len(data) != 0 {
		return EdgeRecord{}, fmt.Errorf("%w: trailing edge bytes", ErrInvalidRecord)
	}
	return r, nil
}
