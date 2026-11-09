package engine

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

const maxWALReplayBytes = 16 << 30

// openReplayWAL validates the full WAL before modifying the primary. Replay
// makes a second bounded-memory pass under the exclusive database file lock.
func openReplayWAL(db, wal *os.File, hdr *Header, opts *Options, pageSize, physicalPageSize int, usePrimaryFdatasync bool, primaryHeaderErr error) (uint64, bool, [32]byte, error) {
	info, err := wal.Stat()
	if err != nil {
		return 0, false, [32]byte{}, err
	}
	if info.Size() > maxWALReplayBytes {
		return 0, false, [32]byte{}, fmt.Errorf("%w: WAL size %d exceeds replay limit", ErrCorruptWAL, info.Size())
	}
	useMAC := hdr.Features&FeatureWALKeyedMAC != 0
	var key, headerKey [32]byte
	if useMAC {
		if opts == nil || !opts.EnableWALMAC {
			return 0, false, key, ErrBadEncryptionKey
		}
		key, headerKey = opts.WALMACKey, opts.HeaderMACKey
	}
	if info.Size() != 0 {
		plan, err := inspectFramedWAL(wal, *hdr, useMAC, key, headerKey, physicalPageSize)
		if err != nil {
			return 0, false, key, err
		}
		if plan.framed {
			legacyPrimary := hdr.FormatVersion == formatVersionV2 && hdr.Features&FeatureWALFrameDigest == 0
			if primaryHeaderErr != nil || legacyPrimary || plan.final.LastWALSeq > hdr.LastWALSeq {
				if primaryHeaderErr == nil && !legacyPrimary && plan.final.CommitSeq < hdr.CommitSeq {
					return 0, false, key, ErrCorruptWAL
				}
				if err := replayFramedWAL(db, wal, hdr, plan, useMAC, key, headerKey, physicalPageSize, usePrimaryFdatasync); err != nil {
					return 0, false, key, err
				}
				primaryHeaderErr = nil
			}
		} else {
			if primaryHeaderErr != nil {
				return 0, false, key, primaryHeaderErr
			}
			if _, err := wal.Seek(0, io.SeekStart); err != nil {
				return 0, false, key, err
			}
			if err := replayWAL(db, wal, hdr, pageSize, physicalPageSize, useMAC, key, headerKey, usePrimaryFdatasync); err != nil {
				return 0, false, key, err
			}
		}
	}
	if primaryHeaderErr != nil {
		return 0, false, key, primaryHeaderErr
	}
	if err := openTruncateWAL(wal); err != nil {
		return 0, false, key, err
	}
	return hdr.LastWALSeq, useMAC, key, nil
}

type walReplayPlan struct {
	base       Header
	final      Header
	headerKey  [32]byte
	lastPage   uint64
	pageCount  uint64
	frameCount uint64
	hasMarker  bool
}

func (p *walReplayPlan) inspect(seq, pageID uint64, payload []byte) error {
	if pageID == 0 {
		return p.inspectMarker(seq, payload)
	}
	if seq <= p.lastPage {
		return ErrCorruptWAL
	}
	if p.base.FormatVersion == formatVersionV3 && (p.hasMarker || binary.BigEndian.Uint64(payload[:8]) != seq) {
		return ErrCorruptWAL
	}
	if p.base.FormatVersion != formatVersionV3 && p.hasMarker &&
		(seq > p.final.LastWALSeq || pageID >= p.final.NextPageID) {
		return ErrCorruptWAL
	}
	p.lastPage = seq
	p.pageCount++
	return nil
}

func (p *walReplayPlan) inspectMarker(seq uint64, payload []byte) error {
	final, err := decodeHeaderPage(payload)
	if err != nil || final.FormatVersion != p.base.FormatVersion ||
		final.PageSize != p.base.PageSize || final.EncryptionSalt != p.base.EncryptionSalt ||
		final.LastWALSeq != seq || final.CommitSeq < p.base.CommitSeq ||
		final.NextPageID == 0 || final.BTreeRoot >= final.NextPageID || p.hasMarker {
		return ErrCorruptWAL
	}
	if p.base.FormatVersion == formatVersionV3 {
		if seq < p.lastPage || !verifyHeaderAuthentication(final, p.headerKey) {
			return ErrCorruptWAL
		}
	} else {
		if p.pageCount != 0 {
			return ErrCorruptWAL
		}
		p.frameCount = binary.BigEndian.Uint64(payload[headerPrefixSize:])
	}
	p.final, p.hasMarker = final, true
	return nil
}

func (p *walReplayPlan) finish(highest uint64) error {
	if highest <= p.base.LastWALSeq {
		p.final = p.base
		return nil
	}
	if !p.hasMarker {
		return fmt.Errorf("%w: pending WAL has no recoverable commit header", ErrCorruptWAL)
	}
	if p.final.LastWALSeq != highest {
		return ErrCorruptWAL
	}
	if p.base.FormatVersion != formatVersionV3 &&
		(p.frameCount != p.pageCount || (p.pageCount != 0 && p.lastPage != highest)) {
		return ErrCorruptWAL
	}
	return nil
}

func replayWAL(db, wal *os.File, hdr *Header, pageSize, physicalPageSize int, useMAC bool, key, headerKey [32]byte, useFdatasync bool) error {
	plan := walReplayPlan{base: *hdr, headerKey: headerKey, lastPage: hdr.LastWALSeq}
	highest, err := scanWAL(wal, hdr.LastWALSeq, func(seq, pageID uint64, payload []byte) error {
		if pageID != 0 {
			if _, err := pageByteOffset(pageID, pageSize, physicalPageSize); err != nil {
				return err
			}
		}
		return plan.inspect(seq, pageID, payload)
	}, key, useMAC, physicalPageSize)
	if err != nil {
		return err
	}
	if err := plan.finish(highest); err != nil {
		return err
	}
	if highest <= hdr.LastWALSeq {
		return nil
	}
	if _, err := wal.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if _, err := scanWAL(wal, hdr.LastWALSeq, func(seq, pageID uint64, payload []byte) error {
		if seq > plan.final.LastWALSeq {
			return ErrCorruptWAL
		}
		if pageID == 0 {
			return nil
		}
		offset, err := pageByteOffset(pageID, pageSize, physicalPageSize)
		if err != nil {
			return err
		}
		_, err = db.WriteAt(payload, offset)
		return err
	}, key, useMAC, physicalPageSize); err != nil {
		return err
	}
	if err := syncFilePrimary(db, useFdatasync); err != nil {
		return err
	}
	if err := writeHeaderAtAuthenticated(db, plan.final, headerKey, hdr.FormatVersion == formatVersionV3); err != nil {
		return err
	}
	if err := syncFilePrimary(db, useFdatasync); err != nil {
		return err
	}
	*hdr = plan.final
	return nil
}

func (e *Engine) encodeHeaderWALRecord(hdr Header, pageCount uint64) []byte {
	if e.headerMACEnabled {
		hdr = authenticateHeader(hdr, e.headerMACKey)
	}
	payload := make([]byte, e.physicalPageSize)
	copy(payload, encodeHeaderPage(hdr))
	if !e.headerMACEnabled {
		binary.BigEndian.PutUint64(payload[headerPrefixSize:], pageCount)
	}
	return encodeWALRecordWithMAC(hdr.LastWALSeq, 0, payload, e.walMACKey, e.walMACEnabled, e.physicalPageSize)
}
