package engine

import "testing"

func FuzzDecodeHeaderPage(f *testing.F) {
	h := Header{
		FormatVersion:  formatVersionV1,
		PageSize:       uint32(DefaultPageSize),
		LastWALSeq:     1,
		NextPageID:     2,
		BTreeRoot:      0,
		Features:       0,
		EncryptionSalt: [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
	}
	f.Add(encodeHeaderPage(h))

	f.Fuzz(func(t *testing.T, data []byte) {
		t.Helper()
		page := make([]byte, DefaultPageSize)
		copy(page, data)
		_, _ = decodeHeaderPage(page)
	})
}

func FuzzParseAndReplayWAL(f *testing.F) {
	payload := make([]byte, DefaultPageSize)
	for i := range payload {
		payload[i] = byte(i)
	}
	f.Add(encodeWALRecord(1, 1, payload, DefaultPageSize))

	f.Fuzz(func(t *testing.T, data []byte) {
		t.Helper()
		_, _ = parseAndReplayWAL(data, 0, func(_, _ uint64, _ []byte) error {
			return nil
		}, DefaultPageSize)
	})
}
