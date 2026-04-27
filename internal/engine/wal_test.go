package engine

import (
	"bytes"
	"errors"
	"testing"
)

func TestWAL_roundTripAndReplayOrder(t *testing.T) {
	t.Parallel()
	payload1 := makeTestPage('a')
	payload2 := makeTestPage('b')
	rec1 := encodeWALRecord(1, 1, payload1, DefaultPageSize)
	rec2 := encodeWALRecord(2, 2, payload2, DefaultPageSize)
	data := append(append([]byte{}, rec1...), rec2...)

	var applied []string
	apply := func(seq, pageID uint64, payload []byte) error {
		applied = append(applied, formatApply(seq, pageID, payload))
		return nil
	}
	maxSeq, err := parseAndReplayWAL(data, 0, apply, DefaultPageSize)
	if err != nil {
		t.Fatal(err)
	}
	if maxSeq != 2 {
		t.Fatalf("maxSeq: got %d want 2", maxSeq)
	}
	if len(applied) != 2 {
		t.Fatalf("applied count: got %d", len(applied))
	}
	if applied[0] != "1:1:a" || applied[1] != "2:2:b" {
		t.Fatalf("order: %v", applied)
	}

	// Idempotent: replay with lastApplied=2 applies nothing new.
	applied = nil
	maxSeq, err = parseAndReplayWAL(data, 2, apply, DefaultPageSize)
	if err != nil {
		t.Fatal(err)
	}
	if maxSeq != 2 {
		t.Fatalf("maxSeq second pass: got %d", maxSeq)
	}
	if len(applied) != 0 {
		t.Fatalf("expected no apply, got %v", applied)
	}
}

func TestWAL_corruptTruncated(t *testing.T) {
	t.Parallel()
	payload := makeTestPage('z')
	rec := encodeWALRecord(1, 1, payload, DefaultPageSize)
	trunc := rec[:len(rec)-10]
	_, err := parseAndReplayWAL(trunc, 0, func(uint64, uint64, []byte) error { return nil }, DefaultPageSize)
	if !errors.Is(err, ErrCorruptWAL) {
		t.Fatalf("want ErrCorruptWAL, got %v", err)
	}
}

func TestWAL_corruptCRC(t *testing.T) {
	t.Parallel()
	payload := makeTestPage('z')
	rec := encodeWALRecord(1, 1, payload, DefaultPageSize)
	rec[len(rec)-1] ^= 0xff
	_, err := parseAndReplayWAL(rec, 0, func(uint64, uint64, []byte) error { return nil }, DefaultPageSize)
	if !errors.Is(err, ErrCorruptWAL) {
		t.Fatalf("want ErrCorruptWAL, got %v", err)
	}
}

func TestWAL_macMismatchRejected(t *testing.T) {
	t.Parallel()
	payload := makeTestPage('m')
	var key [32]byte
	key[0] = 1
	rec := encodeWALRecordWithMAC(1, 1, payload, key, true, DefaultPageSize)
	rec[len(rec)-1] ^= 0xff
	_, err := parseAndReplayWALWithMAC(rec, 0, func(uint64, uint64, []byte) error { return nil }, key, true, DefaultPageSize)
	if !errors.Is(err, ErrCorruptWAL) {
		t.Fatalf("want ErrCorruptWAL, got %v", err)
	}
}

func makeTestPage(fill byte) []byte {
	b := bytes.Repeat([]byte{fill}, DefaultPageSize)
	return b
}

func formatApply(seq, pageID uint64, payload []byte) string {
	// First data byte identifies payload in tests.
	return string(rune('0'+seq)) + ":" + string(rune('0'+pageID)) + ":" + string(payload[0])
}
