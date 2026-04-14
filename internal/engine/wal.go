package engine

import (
	"encoding/binary"
	"hash/crc32"
)

// WAL record: seq(8) + page_id(8) + crc32(4) + payload(PageSize).
const walRecordOverhead = 8 + 8 + 4

func walRecordSize() int {
	return walRecordOverhead + PageSize
}

func encodeWALRecord(seq, pageID uint64, payload []byte) []byte {
	if len(payload) != PageSize {
		panic("engine: wal payload size")
	}
	out := make([]byte, walRecordSize())
	binary.BigEndian.PutUint64(out[0:8], seq)
	binary.BigEndian.PutUint64(out[8:16], pageID)
	binary.BigEndian.PutUint32(out[16:20], crc32.ChecksumIEEE(payload))
	copy(out[20:], payload)
	return out
}

// parseAndReplayWAL scans walData and applies records with seq > lastApplied to db.
// Returns the highest seq value seen in the file (for header update).
func parseAndReplayWAL(walData []byte, lastApplied uint64, apply func(seq, pageID uint64, payload []byte) error) (maxSeq uint64, err error) {
	recSize := walRecordSize()
	for off := 0; off < len(walData); off += recSize {
		if len(walData)-off < recSize {
			return 0, ErrCorruptWAL
		}
		chunk := walData[off : off+recSize]
		seq := binary.BigEndian.Uint64(chunk[0:8])
		pageID := binary.BigEndian.Uint64(chunk[8:16])
		wantCRC := binary.BigEndian.Uint32(chunk[16:20])
		payload := chunk[20:]
		if crc32.ChecksumIEEE(payload) != wantCRC {
			return 0, ErrCorruptWAL
		}
		if seq > maxSeq {
			maxSeq = seq
		}
		if seq <= lastApplied {
			continue
		}
		if pageID < 1 {
			return 0, ErrBadPageID
		}
		if err := apply(seq, pageID, payload); err != nil {
			return 0, err
		}
	}
	return maxSeq, nil
}
