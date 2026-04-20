package record_test

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"reflect"
	"testing"

	"github.com/oklog/ulid/v2"

	"github.com/hexxla/hexxladb/internal/lattice"
	"github.com/hexxla/hexxladb/internal/record"
)

func mustPack(t *testing.T, q, r int) lattice.PackedCoord {
	t.Helper()
	p, err := lattice.Pack(lattice.Coord{Q: q, R: r})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestCell_roundTrip(t *testing.T) {
	t.Parallel()
	key := mustPack(t, 0, 0)
	v := int64(1_700_000_000_000_000_000)
	orig := record.CellRecord{
		Key:        key,
		RawContent: "hello",
		Provenance: record.ProvenanceWire{
			SourceID:   "src",
			Confidence: 0.9,
			CreatedAt:  100,
			UpdatedAt:  200,
		},
		Validity: record.ValidityWire{
			ValidFrom: &v,
			ValidTo:   nil,
		},
		Tags: []string{"a", "b"},
	}
	b, err := record.EncodeCell(orig)
	if err != nil {
		t.Fatal(err)
	}
	got, err := record.DecodeCell(b)
	if err != nil {
		t.Fatal(err)
	}
	if !cellEqual(orig, got) {
		t.Fatalf("got %+v want %+v", got, orig)
	}
}

func cellEqual(a, b record.CellRecord) bool {
	if a.Key != b.Key || a.RawContent != b.RawContent || a.Provenance != b.Provenance {
		return false
	}
	if (a.Validity.ValidFrom == nil) != (b.Validity.ValidFrom == nil) {
		return false
	}
	if a.Validity.ValidFrom != nil && b.Validity.ValidFrom != nil && *a.Validity.ValidFrom != *b.Validity.ValidFrom {
		return false
	}
	if (a.Validity.ValidTo == nil) != (b.Validity.ValidTo == nil) {
		return false
	}
	if a.Validity.ValidTo != nil && b.Validity.ValidTo != nil && *a.Validity.ValidTo != *b.Validity.ValidTo {
		return false
	}
	if len(a.Tags) != len(b.Tags) {
		return false
	}
	for i := range a.Tags {
		if a.Tags[i] != b.Tags[i] {
			return false
		}
	}
	if (a.ClusterHint == nil) != (b.ClusterHint == nil) {
		return false
	}
	if a.ClusterHint != nil && b.ClusterHint != nil && *a.ClusterHint != *b.ClusterHint {
		return false
	}
	return true
}

func TestFacet_roundTrip(t *testing.T) {
	t.Parallel()
	key := mustPack(t, 1, -1)
	raw := []byte("raw cell")
	hash := record.HashRawContent(raw)
	orig := record.FacetRecord{
		Key:            key,
		FacetID:        2,
		DerivedContent: "derived",
		LastRotated:    42,
		DerivationHash: hash,
	}
	b, err := record.EncodeFacet(orig)
	if err != nil {
		t.Fatal(err)
	}
	got, err := record.DecodeFacet(b)
	if err != nil {
		t.Fatal(err)
	}
	if got != orig {
		t.Fatalf("got %+v want %+v", got, orig)
	}
}

func TestEdge_roundTrip(t *testing.T) {
	t.Parallel()
	a := mustPack(t, 0, 0)
	bc := mustPack(t, 1, 0)
	orig := record.EdgeRecord{
		From:         a,
		To:           bc,
		RelationType: "near",
		Weight:       1.5,
		Provenance: record.ProvenanceWire{
			SourceID:   "e",
			Confidence: 1,
			CreatedAt:  3,
			UpdatedAt:  4,
		},
	}
	b, err := record.EncodeEdge(orig)
	if err != nil {
		t.Fatal(err)
	}
	got, err := record.DecodeEdge(b)
	if err != nil {
		t.Fatal(err)
	}
	if got != orig {
		t.Fatalf("got %+v want %+v", got, orig)
	}
}

func TestSeam_roundTrip(t *testing.T) {
	t.Parallel()
	id := ulid.MustNew(ulid.Now(), rand.Reader).String()
	ca := mustPack(t, 0, 0)
	cb := mustPack(t, 0, 1)
	orig := record.SeamRecord{
		ID:               id,
		CellA:            ca,
		CellB:            cb,
		SeamType:         "t",
		Reason:           "r",
		ConfidenceDelta:  0.25,
		DetectedAt:       99,
		ResolutionStatus: "open",
		ResolutionNote:   "",
	}
	b, err := record.EncodeSeam(orig)
	if err != nil {
		t.Fatal(err)
	}
	got, err := record.DecodeSeam(b)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, orig) {
		t.Fatalf("got %+v want %+v", got, orig)
	}
}

func TestSeam_validityRoundTrip(t *testing.T) {
	t.Parallel()
	id := ulid.MustNew(ulid.Now(), rand.Reader).String()
	ca := mustPack(t, 0, 0)
	cb := mustPack(t, 0, 1)
	var lo int64 = 100
	var hi int64 = 200
	orig := record.SeamRecord{
		ID:               id,
		CellA:            ca,
		CellB:            cb,
		SeamType:         "t",
		Reason:           "r",
		ConfidenceDelta:  0.25,
		DetectedAt:       99,
		ResolutionStatus: "open",
		ResolutionNote:   "",
		Validity:         record.ValidityWire{ValidFrom: &lo, ValidTo: &hi},
	}
	b, err := record.EncodeSeam(orig)
	if err != nil {
		t.Fatal(err)
	}
	got, err := record.DecodeSeam(b)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, orig) {
		t.Fatalf("got %+v want %+v", got, orig)
	}
}

func TestSeam_decodeLegacyPayloadWithoutValiditySuffix(t *testing.T) {
	t.Parallel()
	id := ulid.MustNew(ulid.Now(), rand.Reader).String()
	ca := mustPack(t, 0, 0)
	cb := mustPack(t, 0, 1)
	orig := record.SeamRecord{
		ID:               id,
		CellA:            ca,
		CellB:            cb,
		SeamType:         "t",
		Reason:           "r",
		ConfidenceDelta:  0.25,
		DetectedAt:       99,
		ResolutionStatus: "open",
		ResolutionNote:   "",
	}
	full, err := record.EncodeSeam(orig)
	if err != nil {
		t.Fatal(err)
	}
	_, payload, err := record.ParseEnvelope(record.MagicSeam, full)
	if err != nil {
		t.Fatal(err)
	}
	// Strip empty ValidityWire (2 bytes) + empty ProvenanceWire (str32 + 3×int64) — simulates payloads that ended at ResolutionNote.
	const emptyValidityAndProvenanceTail = 2 + (4 + 8 + 8 + 8)
	if len(payload) < emptyValidityAndProvenanceTail {
		t.Fatalf("short payload len=%d", len(payload))
	}
	legacyPayload := payload[:len(payload)-emptyValidityAndProvenanceTail]
	legacy, err := record.AppendEnvelope(nil, record.MagicSeam, record.FormatVersionV1, legacyPayload)
	if err != nil {
		t.Fatal(err)
	}
	got, err := record.DecodeSeam(legacy)
	if err != nil {
		t.Fatal(err)
	}
	want := orig
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v want %+v", got, want)
	}
}

func TestSeam_provenanceRoundTrip(t *testing.T) {
	t.Parallel()
	id := ulid.MustNew(ulid.Now(), rand.Reader).String()
	ca := mustPack(t, 0, 0)
	cb := mustPack(t, 0, 1)
	orig := record.SeamRecord{
		ID:               id,
		CellA:            ca,
		CellB:            cb,
		SeamType:         "t",
		Reason:           "r",
		ConfidenceDelta:  0.25,
		DetectedAt:       99,
		ResolutionStatus: "open",
		ResolutionNote:   "",
		Provenance: record.ProvenanceWire{
			SourceID:   "sensor-1",
			Confidence: 0.9,
			CreatedAt:  1,
			UpdatedAt:  2,
		},
	}
	b, err := record.EncodeSeam(orig)
	if err != nil {
		t.Fatal(err)
	}
	got, err := record.DecodeSeam(b)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, orig) {
		t.Fatalf("got %+v want %+v", got, orig)
	}
}

func TestSeam_invalidULID(t *testing.T) {
	t.Parallel()
	_, err := record.EncodeSeam(record.SeamRecord{
		ID:               "not-a-ulid",
		CellA:            mustPack(t, 0, 0),
		CellB:            mustPack(t, 0, 0),
		SeamType:         "x",
		Reason:           "y",
		ConfidenceDelta:  0,
		DetectedAt:       0,
		ResolutionStatus: "z",
		ResolutionNote:   "",
	})
	if !errors.Is(err, record.ErrInvalidULID) {
		t.Fatalf("want ErrInvalidULID, got %v", err)
	}
}

func TestParseEnvelope_unsupportedFormatVersion(t *testing.T) {
	t.Parallel()
	payload := []byte{1, 2, 3}
	var hdr [10]byte
	copy(hdr[0:4], record.MagicCell[:])
	binary.BigEndian.PutUint16(hdr[4:6], 999) // unsupported
	binary.BigEndian.PutUint32(hdr[6:10], uint32(len(payload)))
	data := append(hdr[:], payload...)
	_, _, err := record.ParseEnvelope(record.MagicCell, data)
	if !errors.Is(err, record.ErrUnsupportedFormatVersion) {
		t.Fatalf("want ErrUnsupportedFormatVersion, got %v", err)
	}
}

func TestCell_golden(t *testing.T) {
	t.Parallel()
	key := mustPack(t, 0, 0)
	r := record.CellRecord{
		Key:        key,
		RawContent: "x",
		Provenance: record.ProvenanceWire{
			SourceID:   "s",
			Confidence: 1,
			CreatedAt:  0,
			UpdatedAt:  0,
		},
		Validity: record.ValidityWire{},
		Tags:     nil,
	}
	got, err := record.EncodeCell(r)
	if err != nil {
		t.Fatal(err)
	}
	// If this fails, v1 envelope or cell layout changed — update FORMAT.md.
	wantPrefix := []byte{'H', 'X', 'C', 'L', 0, 1}
	if len(got) < 10 {
		t.Fatal("short")
	}
	if !bytes.HasPrefix(got, wantPrefix) {
		t.Fatalf("header mismatch got %v want prefix %v", got[:6], wantPrefix)
	}
	payloadLen := binary.BigEndian.Uint32(got[6:10])
	if int(payloadLen) != len(got)-10 {
		t.Fatalf("payload len field %d vs actual %d", payloadLen, len(got)-10)
	}
	dec, err := record.DecodeCell(got)
	if err != nil {
		t.Fatal(err)
	}
	if !cellEqual(r, dec) {
		t.Fatalf("round trip %+v vs %+v", r, dec)
	}
}

func TestCanonicalCellPair(t *testing.T) {
	t.Parallel()
	a := mustPack(t, 0, 0)
	b := mustPack(t, 1, 0)
	lo, hi := record.CanonicalCellPair(a, b)
	if lo != a || hi != b {
		t.Fatalf("order a,b got lo=%v hi=%v", lo, hi)
	}
	lo2, hi2 := record.CanonicalCellPair(b, a)
	if lo2 != a || hi2 != b {
		t.Fatalf("order b,a got lo=%v hi=%v", lo2, hi2)
	}
}
