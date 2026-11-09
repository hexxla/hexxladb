package engine

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"hash"
	"io"
	"os"
)

const walFrameMagic = "HXWAL002"
const walFrameMagicOffset = headerPrefixSize + 8
const walFrameDigestOffset = walFrameMagicOffset + len(walFrameMagic)

func hashWALPage(digest hash.Hash, seq, pageID uint64, payload []byte) {
	var prefix [16]byte
	binary.BigEndian.PutUint64(prefix[:8], seq)
	binary.BigEndian.PutUint64(prefix[8:], pageID)
	_, _ = digest.Write(prefix[:])
	_, _ = digest.Write(payload)
}

func (e *Engine) encodeFramedHeaderWALRecord(hdr Header, pending []pendingRedo) []byte {
	hdr.Features |= FeatureWALFrameDigest
	if e.headerMACEnabled {
		hdr = authenticateHeader(hdr, e.headerMACKey)
	}
	payload := make([]byte, e.physicalPageSize)
	copy(payload, encodeHeaderPage(hdr))
	binary.BigEndian.PutUint64(payload[headerPrefixSize:], uint64(len(pending)))
	copy(payload[walFrameMagicOffset:], walFrameMagic)
	digest := sha256.New()
	for _, page := range pending {
		hashWALPage(digest, page.seq, page.pageID, page.plain)
	}
	copy(payload[walFrameDigestOffset:], digest.Sum(nil))
	return encodeWALRecordWithMAC(hdr.LastWALSeq, 0, payload, e.walMACKey, e.walMACEnabled, e.physicalPageSize)
}

// readHeaderForRecovery keeps the primary's immutable identity as the recovery
// trust anchor. A bad mutable checksum/MAC may be repaired only by a complete
// framed WAL bound to that identity. Invalid magic/layout cannot be guessed.
func readHeaderForRecovery(db *os.File, pageSize int) (Header, error, error) {
	page := make([]byte, pageSize)
	if _, err := db.ReadAt(page, 0); err != nil {
		return Header{}, nil, err
	}
	hdr, err := decodeHeaderFields(page)
	if err != nil {
		return Header{}, nil, err
	}
	// Reserved bytes cannot differ between old and new authenticated headers.
	if hdr.FormatVersion == formatVersionV3 {
		for _, b := range page[headerReservedOffset:headerPrefixSize] {
			if b != 0 {
				return Header{}, nil, fmt.Errorf("%w: nonzero reserved header byte", ErrCorruptHeader)
			}
		}
	}
	return hdr, validateHeaderIntegrity(page, hdr), nil
}

type framedWAL struct {
	final       Header
	count       uint64
	markerIndex uint64
	framed      bool
}

// inspectFramedWAL checks every physical record, including the reusable old
// extent, before any primary writes. The commit descriptor binds the ordered
// page ids, generations, images and exact count; a marker alone is insufficient.
func inspectFramedWAL(wal *os.File, base Header, useMAC bool, key, headerKey [32]byte, physicalPageSize int) (framedWAL, error) {
	var plan framedWAL
	var index, pages, lastSeq, maxPage, highest uint64
	digest := sha256.New()
	var wantedDigest []byte
	var pageErr error
	_, err := scanWALRecords(wal, func(seq, pageID uint64, payload []byte) error {
		current := index
		index++
		highest = max(highest, seq)
		if plan.framed && current > plan.markerIndex && base.FormatVersion == formatVersionV3 {
			if seq > plan.final.LastWALSeq {
				return ErrCorruptWAL
			}
			return nil
		}
		if plan.framed && base.FormatVersion == formatVersionV2 && pages == plan.count {
			if seq > plan.final.LastWALSeq {
				return ErrCorruptWAL
			}
			return nil
		}
		if pageID == 0 {
			final, decodeErr := decodeHeaderPage(payload)
			if decodeErr != nil {
				return ErrCorruptWAL
			}
			if final.Features&FeatureWALFrameDigest == 0 {
				// An established framed database cannot be downgraded by stripping the
				// descriptor or substituting a legacy marker for a pending commit.
				if base.Features&FeatureWALFrameDigest != 0 && seq > base.LastWALSeq {
					return ErrCorruptWAL
				}
				return nil
			}
			if plan.framed || final.LastWALSeq != seq ||
				final.FormatVersion != base.FormatVersion || final.PageSize != base.PageSize ||
				final.EncryptionSalt != base.EncryptionSalt || final.EncryptionKeyCheck != base.EncryptionKeyCheck ||
				(final.Features&(FeatureEncryptedDataPages|FeatureWALKeyedMAC|FeatureAuthenticatedDataPages)) !=
					(base.Features&(FeatureEncryptedDataPages|FeatureWALKeyedMAC|FeatureAuthenticatedDataPages)) ||
				!bytes.Equal(payload[walFrameMagicOffset:walFrameDigestOffset], []byte(walFrameMagic)) {
				return ErrCorruptWAL
			}
			if base.FormatVersion == formatVersionV3 && !verifyHeaderAuthentication(final, headerKey) {
				return ErrCorruptWAL
			}
			count := binary.BigEndian.Uint64(payload[headerPrefixSize:])
			if count > uint64(maxWALReplayBytes)/uint64(walRecordSize(physicalPageSize)) { //nolint:gosec // physicalPageSize is a validated positive page size plus bounded envelope overhead.
				return ErrCorruptWAL
			}
			if base.FormatVersion == formatVersionV2 && current != 0 {
				return ErrCorruptWAL
			}
			if base.FormatVersion == formatVersionV3 && current != count {
				return ErrCorruptWAL
			}
			plan = framedWAL{final: final, count: count, markerIndex: current, framed: true}
			wantedDigest = bytes.Clone(payload[walFrameDigestOffset : walFrameDigestOffset+sha256.Size])
			return nil
		}
		// Legacy WALs are validated by the original reader below. The same
		// accumulator can recognize a framed trailing v3 marker without buffering.
		if seq <= lastSeq {
			pageErr = ErrCorruptWAL
		}
		if base.FormatVersion == formatVersionV3 && binary.BigEndian.Uint64(payload[:8]) != seq {
			pageErr = ErrCorruptWAL
		}
		if _, err := pageByteOffset(pageID, int(base.PageSize), physicalPageSize); err != nil {
			pageErr = err
		}
		lastSeq, maxPage = seq, max(maxPage, pageID)
		pages++
		hashWALPage(digest, seq, pageID, payload)
		return nil
	}, key, useMAC, physicalPageSize)
	if err != nil {
		return plan, err
	}
	if !plan.framed {
		return plan, nil
	}
	if pageErr != nil {
		return plan, pageErr
	}
	if plan.final.LastWALSeq == 0 || pages != plan.count || highest != plan.final.LastWALSeq || maxPage >= plan.final.NextPageID ||
		(pages != 0 && lastSeq != plan.final.LastWALSeq) || !bytes.Equal(wantedDigest, digest.Sum(nil)) {
		return plan, ErrCorruptWAL
	}
	return plan, nil
}

func replayFramedWAL(db, wal *os.File, base *Header, plan framedWAL, useMAC bool, key, headerKey [32]byte, physicalPageSize int, useFdatasync bool) error {
	if _, err := wal.Seek(0, io.SeekStart); err != nil {
		return err
	}
	var index uint64
	if _, err := scanWALRecords(wal, func(_ uint64, pageID uint64, payload []byte) error {
		current := index
		index++
		active := current < plan.count
		if plan.final.FormatVersion == formatVersionV2 {
			active = current > 0 && current <= plan.count
		}
		if !active || pageID == 0 {
			return nil
		}
		off, err := pageByteOffset(pageID, int(plan.final.PageSize), physicalPageSize)
		if err != nil {
			return err
		}
		_, err = db.WriteAt(payload, off)
		return err
	}, key, useMAC, physicalPageSize); err != nil {
		return err
	}
	if err := syncFilePrimary(db, useFdatasync); err != nil {
		return err
	}
	if err := writeHeaderAtAuthenticated(db, plan.final, headerKey, plan.final.FormatVersion == formatVersionV3); err != nil {
		return err
	}
	if err := syncFilePrimary(db, useFdatasync); err != nil {
		return err
	}
	*base = plan.final
	return nil
}
