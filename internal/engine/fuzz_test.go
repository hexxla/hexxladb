package engine

import (
	"bytes"
	"errors"
	"testing"
)

func FuzzDecodeHeaderPage(f *testing.F) {
	h := Header{
		FormatVersion:  formatVersionV2,
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
	// Each three-byte input describes a sequence, a page ID, and a page fill
	// byte. Keep the corpus compact while exercising real-sized WAL records
	// with valid checksums and MACs before applying deliberate corruption.
	frames := []byte{1, 1, 0xAB, 3, 2, 0xCD, 2, 1, 0xEF}
	f.Add([]byte{}, uint8(0), uint8(0), uint8(0), false)
	f.Add(frames, uint8(1), uint8(0), uint8(0), false)
	f.Add(frames, uint8(1), uint8(0), uint8(0), true)
	f.Add(frames, uint8(0), uint8(1), uint8(0), false)
	f.Add(frames, uint8(0), uint8(2), uint8(1), false)
	f.Add(frames, uint8(0), uint8(3), uint8(1), true)

	f.Fuzz(func(t *testing.T, input []byte, lastApplied, damage, target uint8, useMAC bool) {
		const bytesPerFrame = 3
		const maxFrames = 8
		key := [32]byte{0xFE, 0x19}
		frameCount := min(len(input)/bytesPerFrame, maxFrames)
		recordSize := walRecordSize(DefaultPageSize)
		if useMAC {
			recordSize += walRecordMACSize
		}
		data := make([]byte, 0, frameCount*recordSize)
		type replayFrame struct {
			seq, pageID uint64
			payload     []byte
		}
		frames := make([]replayFrame, frameCount)
		for i := range frames {
			frame := input[i*bytesPerFrame : (i+1)*bytesPerFrame]
			payload := bytes.Repeat(frame[2:3], DefaultPageSize)
			frames[i] = replayFrame{seq: uint64(frame[0]), pageID: uint64(frame[1]), payload: payload}
			data = append(data, encodeWALRecordWithMAC(frames[i].seq, frames[i].pageID, payload, key, useMAC, DefaultPageSize)...)
		}

		validFrames := frameCount
		if frameCount > 0 {
			frame := int(target) % frameCount
			switch damage % 4 {
			case 1:
				data = data[:len(data)-1]
				validFrames = frameCount - 1
			case 2:
				data[frame*recordSize+walRecordOverhead] ^= 1
				validFrames = frame
			case 3:
				offset := 16 // CRC field
				if useMAC {
					offset = 8 // page ID authenticated by the MAC
				}
				data[frame*recordSize+offset] ^= 1
				validFrames = frame
			}
		}

		var expectedMax uint64
		expected := make([]replayFrame, 0, validFrames)
		for _, frame := range frames[:validFrames] {
			expectedMax = max(expectedMax, frame.seq)
			if frame.seq > uint64(lastApplied) {
				expected = append(expected, frame)
			}
		}
		gotMax, err := parseAndReplayWALWithMAC(data, uint64(lastApplied), func(seq, pageID uint64, payload []byte) error {
			if len(expected) == 0 {
				t.Fatalf("unexpected replay of sequence %d, page %d", seq, pageID)
			}
			want := expected[0]
			expected = expected[1:]
			if seq != want.seq || pageID != want.pageID || !bytes.Equal(payload, want.payload) {
				t.Fatalf("wrong replay: got sequence %d, page %d; want sequence %d, page %d and identical payload", seq, pageID, want.seq, want.pageID)
			}
			return nil
		}, key, useMAC, DefaultPageSize)
		if len(expected) != 0 {
			t.Fatalf("missed %d replay records", len(expected))
		}
		if validFrames < frameCount {
			if !errors.Is(err, ErrCorruptWAL) || gotMax != 0 {
				t.Fatalf("corrupt WAL: got sequence %d, error %v", gotMax, err)
			}
			return
		}
		if err != nil || gotMax != expectedMax {
			t.Fatalf("valid WAL: got sequence %d, error %v; want sequence %d", gotMax, err, expectedMax)
		}
	})
}
