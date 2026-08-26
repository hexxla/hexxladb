package engine

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"hash/crc32"
)

// WAL record: seq(8) + page_id(8) + crc32(4) + payload(pageSize).
const walRecordOverhead = 8 + 8 + 4
const walRecordMACSize = 32

func walRecordSize(pageSize int) int {
	return walRecordOverhead + pageSize
}

func encodeWALRecord(seq, pageID uint64, payload []byte, pageSize int) []byte {
	return encodeWALRecordWithMAC(seq, pageID, payload, [32]byte{}, false, pageSize)
}

func encodeWALRecordWithMAC(seq, pageID uint64, payload []byte, macKey [32]byte, useMAC bool, pageSize int) []byte {
	if len(payload) != pageSize {
		panic("engine: wal payload size mismatch")
	}
	size := walRecordSize(pageSize)
	if useMAC {
		size += walRecordMACSize
	}
	out := make([]byte, size)
	binary.BigEndian.PutUint64(out[0:8], seq)
	binary.BigEndian.PutUint64(out[8:16], pageID)
	binary.BigEndian.PutUint32(out[16:20], crc32.ChecksumIEEE(payload))
	copy(out[20:], payload)
	if useMAC {
		tag := walMAC(sumMACInput(seq, pageID, payload), macKey)
		copy(out[20+pageSize:], tag[:])
	}
	return out
}

// parseAndReplayWAL scans walData and applies records with seq > lastApplied to db.
// Returns the highest seq value seen in the file (for header update).
func parseAndReplayWAL(walData []byte, lastApplied uint64, apply func(seq, pageID uint64, payload []byte) error, pageSize int) (maxSeq uint64, err error) {
	return parseAndReplayWALWithMAC(walData, lastApplied, apply, [32]byte{}, false, pageSize)
}

func parseAndReplayWALWithMAC(
	walData []byte,
	lastApplied uint64,
	apply func(seq, pageID uint64, payload []byte) error,
	macKey [32]byte,
	useMAC bool,
	pageSize int,
) (maxSeq uint64, err error) {
	recSize := walRecordSize(pageSize)
	if useMAC {
		recSize += walRecordMACSize
	}
	for off := 0; off < len(walData); off += recSize {
		if len(walData)-off < recSize {
			return 0, ErrCorruptWAL
		}
		chunk := walData[off : off+recSize]
		seq := binary.BigEndian.Uint64(chunk[0:8])
		pageID := binary.BigEndian.Uint64(chunk[8:16])
		wantCRC := binary.BigEndian.Uint32(chunk[16:20])
		payload := chunk[20:]
		if useMAC {
			if len(payload) < pageSize+walRecordMACSize {
				return 0, ErrCorruptWAL
			}
			tagOff := 20 + pageSize
			payloadData := payload[:pageSize]
			var gotTag [walRecordMACSize]byte
			copy(gotTag[:], chunk[tagOff:tagOff+walRecordMACSize])
			wantTag := walMAC(sumMACInput(seq, pageID, payloadData), macKey)
			if !hmac.Equal(gotTag[:], wantTag[:]) {
				return 0, ErrCorruptWAL
			}
			payload = payloadData
		}
		if crc32.ChecksumIEEE(payload) != wantCRC {
			return 0, ErrCorruptWAL
		}
		if seq > maxSeq {
			maxSeq = seq
		}
		if seq <= lastApplied {
			continue
		}
		if err := apply(seq, pageID, payload); err != nil {
			return 0, err
		}
	}
	return maxSeq, nil
}

func sumMACInput(seq, pageID uint64, payload []byte) []byte {
	in := make([]byte, 16+len(payload))
	binary.BigEndian.PutUint64(in[0:8], seq)
	binary.BigEndian.PutUint64(in[8:16], pageID)
	copy(in[16:], payload)
	return in
}

func walMAC(input []byte, key [32]byte) [walRecordMACSize]byte {
	m := hmac.New(sha256.New, key[:])
	_, _ = m.Write(input)
	sum := m.Sum(nil)
	var out [walRecordMACSize]byte
	copy(out[:], sum[:walRecordMACSize])
	return out
}
