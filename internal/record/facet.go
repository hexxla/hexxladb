package record

import "fmt"

// EncodeFacet encodes a facet record (envelope + v1 payload).
func EncodeFacet(r FacetRecord) ([]byte, error) {
	if r.FacetID > 5 {
		return nil, fmt.Errorf("%w: facet id out of range", ErrInvalidRecord)
	}
	payload, err := encodeFacetPayloadV1(r)
	if err != nil {
		return nil, err
	}
	return AppendEnvelope(nil, MagicFacet, FormatVersionV1, payload)
}

// DecodeFacet decodes a full facet record blob.
func DecodeFacet(data []byte) (FacetRecord, error) {
	_, payload, err := ParseEnvelope(MagicFacet, data)
	if err != nil {
		return FacetRecord{}, err
	}
	return decodeFacetPayloadV1(payload)
}

func encodeFacetPayloadV1(r FacetRecord) ([]byte, error) {
	dst := appendPackedCoord(nil, r.Key)
	dst = append(dst, r.FacetID)
	var err error
	dst, err = appendString32(dst, r.DerivedContent)
	if err != nil {
		return nil, err
	}
	dst = appendInt64BE(dst, r.LastRotated)
	dst = append(dst, r.DerivationHash[:]...)
	return dst, nil
}

func decodeFacetPayloadV1(data []byte) (FacetRecord, error) {
	var r FacetRecord
	var err error
	r.Key, data, err = readPackedCoord(data)
	if err != nil {
		return FacetRecord{}, err
	}
	if len(data) < 1 {
		return FacetRecord{}, fmt.Errorf("%w: facet id", ErrInvalidRecord)
	}
	r.FacetID = data[0]
	if r.FacetID > 5 {
		return FacetRecord{}, fmt.Errorf("%w: facet id out of range", ErrInvalidRecord)
	}
	data = data[1:]
	r.DerivedContent, data, err = readString32(data)
	if err != nil {
		return FacetRecord{}, err
	}
	r.LastRotated, data, err = readInt64BE(data)
	if err != nil {
		return FacetRecord{}, err
	}
	if len(data) != 32 {
		return FacetRecord{}, fmt.Errorf("%w: derivation hash", ErrInvalidRecord)
	}
	copy(r.DerivationHash[:], data[0:32])
	data = data[32:]
	if len(data) != 0 {
		return FacetRecord{}, fmt.Errorf("%w: trailing facet bytes", ErrInvalidRecord)
	}
	return r, nil
}
