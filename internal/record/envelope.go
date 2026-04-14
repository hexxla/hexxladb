package record

import (
	"encoding/binary"
	"fmt"
)

// AppendEnvelope appends a full record: header (magic, formatVersion BE, payloadLen BE) + payload.
func AppendEnvelope(dst []byte, magic [4]byte, formatVersion uint16, payload []byte) ([]byte, error) {
	if len(payload) > MaxPayload {
		return dst, fmt.Errorf("%w: payload too large", ErrInvalidRecord)
	}
	var hdr [headerSize]byte
	copy(hdr[:4], magic[:])
	binary.BigEndian.PutUint16(hdr[4:6], formatVersion)
	u, err := uint32FromInt(len(payload))
	if err != nil {
		return dst, err
	}
	binary.BigEndian.PutUint32(hdr[6:10], u)
	dst = append(dst, hdr[:]...)
	dst = append(dst, payload...)
	return dst, nil
}

// ParseEnvelope splits a full record into magic, format version, and payload.
// It validates length and applies format_version policy.
func ParseEnvelope(magicWant [4]byte, data []byte) (formatVersion uint16, payload []byte, err error) {
	if len(data) < headerSize {
		return 0, nil, fmt.Errorf("%w: truncated header", ErrInvalidRecord)
	}
	var magic [4]byte
	copy(magic[:], data[0:4])
	if magic != magicWant {
		return 0, nil, ErrWrongMagic
	}
	formatVersion = binary.BigEndian.Uint16(data[4:6])
	n := binary.BigEndian.Uint32(data[6:10])
	if n > MaxPayload {
		return 0, nil, fmt.Errorf("%w: payload too large", ErrInvalidRecord)
	}
	if uint64(len(data)) != uint64(headerSize)+uint64(n) {
		return 0, nil, fmt.Errorf("%w: length mismatch", ErrInvalidRecord)
	}
	payload = data[headerSize:]
	switch {
	case formatVersion < FormatVersionV1:
		return 0, nil, ErrUnknownFormatVersion
	case formatVersion > SupportedFormatVersion:
		return 0, nil, fmt.Errorf("%w (format %d)", ErrUnsupportedFormatVersion, formatVersion)
	default:
		return formatVersion, payload, nil
	}
}
