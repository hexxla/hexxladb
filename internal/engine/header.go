package engine

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

// Header is the 512-byte prefix of page 0 (see ENGINE_FORMAT.md).
type Header struct {
	FormatVersion uint32
	PageSize      uint32
	LastWALSeq    uint64
	NextPageID    uint64
	// BTreeRoot is the B+ tree root page id (0 = empty tree). See ORDERED_STORE.md.
	BTreeRoot uint64
	// Features bit field (offset 40). Bit 0 = data pages use at-rest encryption (AES-XTS via hooks).
	Features uint32
	// EncryptionSalt is used for Argon2id passphrase KDF when Features&FeatureEncryptedDataPages; otherwise zeros.
	EncryptionSalt [16]byte
	// CommitSeq is the last assigned logical commit sequence (MVCC). On-disk at offset 60 for format v2; v1 treats as 0.
	CommitSeq uint64
	// EncryptionKeyCheck is a keyed verifier for deterministic wrong-key detection on encrypted DBs.
	EncryptionKeyCheck [HeaderEncryptionKeyCheckLen]byte
}

// FeatureEncryptedDataPages marks btree data pages (page_id >= 1) as encrypted on disk and in the WAL.
const FeatureEncryptedDataPages uint32 = 1 << 0

// FeatureWALKeyedMAC marks WAL records as carrying a keyed MAC trailer for tamper detection.
const FeatureWALKeyedMAC uint32 = 1 << 1

func decodeHeaderPage(page []byte) (Header, error) {
	if len(page) < headerPrefixSize {
		return Header{}, fmt.Errorf("%w: short page", ErrCorruptHeader)
	}
	if !bytes.HasPrefix(page, []byte(headerMagic)) {
		return Header{}, ErrCorruptHeader
	}
	h := Header{
		FormatVersion: binary.BigEndian.Uint32(page[8:12]),
		PageSize:      binary.BigEndian.Uint32(page[12:16]),
		LastWALSeq:    binary.BigEndian.Uint64(page[16:24]),
		NextPageID:    binary.BigEndian.Uint64(page[24:32]),
		BTreeRoot:     binary.BigEndian.Uint64(page[32:40]),
		Features:      binary.BigEndian.Uint32(page[40:44]),
	}
	copy(h.EncryptionSalt[:], page[44:60])
	copy(h.EncryptionKeyCheck[:], page[HeaderEncryptionKeyCheckOffset:HeaderEncryptionKeyCheckOffset+HeaderEncryptionKeyCheckLen])
	switch h.FormatVersion {
	case formatVersionV1:
		h.CommitSeq = 0
	case formatVersionV2:
		h.CommitSeq = binary.BigEndian.Uint64(page[HeaderCommitSeqOffset : HeaderCommitSeqOffset+8])
	default:
		return Header{}, fmt.Errorf("%w: version %d", ErrCorruptHeader, h.FormatVersion)
	}
	if h.PageSize != uint32(PageSize) {
		return Header{}, ErrCorruptHeader
	}
	return h, nil
}

func encodeHeaderPage(h Header) []byte {
	page := make([]byte, PageSize)
	copy(page[:8], headerMagic)
	binary.BigEndian.PutUint32(page[8:12], h.FormatVersion)
	binary.BigEndian.PutUint32(page[12:16], h.PageSize)
	binary.BigEndian.PutUint64(page[16:24], h.LastWALSeq)
	binary.BigEndian.PutUint64(page[24:32], h.NextPageID)
	binary.BigEndian.PutUint64(page[32:40], h.BTreeRoot)
	binary.BigEndian.PutUint32(page[40:44], h.Features)
	copy(page[44:60], h.EncryptionSalt[:])
	if h.FormatVersion == formatVersionV2 {
		binary.BigEndian.PutUint64(page[HeaderCommitSeqOffset:HeaderCommitSeqOffset+8], h.CommitSeq)
	} else {
		binary.BigEndian.PutUint64(page[HeaderCommitSeqOffset:HeaderCommitSeqOffset+8], 0)
	}
	copy(page[HeaderEncryptionKeyCheckOffset:HeaderEncryptionKeyCheckOffset+HeaderEncryptionKeyCheckLen], h.EncryptionKeyCheck[:])
	return page
}

func readHeaderAt(r io.ReaderAt) (Header, error) {
	buf := make([]byte, PageSize)
	n, err := r.ReadAt(buf, 0)
	if err != nil {
		return Header{}, err
	}
	if n != PageSize {
		return Header{}, fmt.Errorf("%w: short read", ErrCorruptHeader)
	}
	return decodeHeaderPage(buf)
}

func writeHeaderAt(w io.WriterAt, h Header) error {
	page := encodeHeaderPage(h)
	if len(page) != PageSize {
		panic("engine: header page size")
	}
	_, err := w.WriteAt(page, 0)
	return err
}

// ReadHeaderFile reads the [Header] from an existing database file without opening an [Engine].
func ReadHeaderFile(path string) (Header, error) {
	// #nosec G304 -- path is caller-chosen (same contract as Open).
	f, err := os.Open(path)
	if err != nil {
		return Header{}, err
	}
	defer func() { _ = f.Close() }()
	return readHeaderAt(f)
}
