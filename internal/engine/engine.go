package engine

import (
	"crypto/rand"
	"fmt"
	"io"
	"os"
)

// Engine is a minimal page-oriented store with redo WAL (M3 shell).
type Engine struct {
	path          string
	db            *os.File
	wal           *os.File
	hooks         PageHooks
	lastSeq       uint64
	walMACEnabled bool
	walMACKey     [32]byte
}

// WalPath returns the WAL path for a primary database path.
func WalPath(primary string) string {
	return primary + "-wal"
}

// Open opens or creates a database at path and replays the WAL.
func Open(path string, opts *Options) (*Engine, error) {
	var hooks PageHooks
	if opts != nil && opts.Hooks != nil {
		hooks = *opts.Hooks
	}

	// #nosec G304 -- path is the caller-chosen database file (public Open API).
	db, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}

	st, err := db.Stat()
	if err != nil {
		_ = db.Close()
		return nil, err
	}

	if st.Size() == 0 {
		ver := formatVersionV1
		if opts != nil && opts.UseFormatV2 {
			ver = formatVersionV2
		}
		hdr := Header{
			FormatVersion: ver,
			PageSize:      uint32(PageSize),
			LastWALSeq:    0,
			NextPageID:    1,
			CommitSeq:     0,
		}
		if opts != nil && opts.NewEncryptedDB {
			hdr.Features |= FeatureEncryptedDataPages
			if opts.EnableWALMAC {
				hdr.Features |= FeatureWALKeyedMAC
			}
			if opts.EncryptionSalt == ([16]byte{}) {
				if _, err := rand.Read(hdr.EncryptionSalt[:]); err != nil {
					_ = db.Close()
					return nil, err
				}
			} else {
				hdr.EncryptionSalt = opts.EncryptionSalt
			}
			hdr.EncryptionKeyCheck = opts.EncryptionKeyCheck
		}
		if err := writeHeaderAt(db, hdr); err != nil {
			_ = db.Close()
			return nil, err
		}
		if err := db.Sync(); err != nil {
			_ = db.Close()
			return nil, err
		}
	} else if st.Size() < PageSize {
		_ = db.Close()
		return nil, ErrCorruptHeader
	}

	hdr, err := readHeaderAt(db)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	if opts != nil && opts.ExpectEncryptionKeyCheck && hdr.Features&FeatureEncryptedDataPages != 0 {
		if hdr.EncryptionKeyCheck != ([HeaderEncryptionKeyCheckLen]byte{}) && hdr.EncryptionKeyCheck != opts.EncryptionKeyCheck {
			_ = db.Close()
			return nil, ErrBadEncryptionKey
		}
	}

	walPath := WalPath(path)
	// #nosec G304 -- WAL path is derived from the primary DB path (same contract as primary).
	wal, err := os.OpenFile(walPath, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		_ = db.Close()
		return nil, err
	}

	// Replay the full WAL. A fixed 1 GiB read cap truncated large sessions and made
	// replay fail with ErrCorruptWAL (partial tail). Cap allocation at 16 GiB instead.
	const maxWALReplayBytes = 16 << 30
	walInfo, err := wal.Stat()
	if err != nil {
		_ = db.Close()
		_ = wal.Close()
		return nil, err
	}
	if walInfo.Size() > maxWALReplayBytes {
		_ = db.Close()
		_ = wal.Close()
		return nil, fmt.Errorf("%w: WAL size %d exceeds max replay size", ErrCorruptWAL, walInfo.Size())
	}
	if _, err := wal.Seek(0, io.SeekStart); err != nil {
		_ = db.Close()
		_ = wal.Close()
		return nil, err
	}
	walData := make([]byte, walInfo.Size())
	if len(walData) > 0 {
		if _, err := io.ReadFull(wal, walData); err != nil {
			_ = db.Close()
			_ = wal.Close()
			return nil, err
		}
	}

	apply := func(_ uint64, pageID uint64, payload []byte) error {
		off, err := pageByteOffset(pageID)
		if err != nil {
			return err
		}
		_, err = db.WriteAt(payload, off)
		return err
	}

	walMACEnabled := hdr.Features&FeatureWALKeyedMAC != 0
	var walMACKey [32]byte
	if walMACEnabled {
		if opts == nil || !opts.EnableWALMAC {
			_ = db.Close()
			_ = wal.Close()
			return nil, ErrBadEncryptionKey
		}
		walMACKey = opts.WALMACKey
	}
	maxSeq, err := parseAndReplayWALWithMAC(walData, hdr.LastWALSeq, apply, walMACKey, walMACEnabled)
	if err != nil {
		_ = db.Close()
		_ = wal.Close()
		return nil, err
	}

	newLast := max(hdr.LastWALSeq, maxSeq)
	if newLast != hdr.LastWALSeq {
		hdr.LastWALSeq = newLast
		if err := writeHeaderAt(db, hdr); err != nil {
			_ = db.Close()
			_ = wal.Close()
			return nil, err
		}
		if err := db.Sync(); err != nil {
			_ = db.Close()
			_ = wal.Close()
			return nil, err
		}
	}

	if err := wal.Truncate(0); err != nil {
		_ = db.Close()
		_ = wal.Close()
		return nil, err
	}
	if _, err := wal.Seek(0, io.SeekStart); err != nil {
		_ = db.Close()
		_ = wal.Close()
		return nil, err
	}
	if err := wal.Sync(); err != nil {
		_ = db.Close()
		_ = wal.Close()
		return nil, err
	}

	e := &Engine{
		path:          path,
		db:            db,
		wal:           wal,
		hooks:         hooks,
		lastSeq:       newLast,
		walMACEnabled: walMACEnabled,
		walMACKey:     walMACKey,
	}
	return e, nil
}

// Close releases file handles.
func (e *Engine) Close() error {
	var errs []error
	if e.wal != nil {
		errs = append(errs, e.wal.Close())
		e.wal = nil
	}
	if e.db != nil {
		errs = append(errs, e.db.Close())
		e.db = nil
	}
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

// ReadPage returns page pageID (0 = header page including meta prefix).
func (e *Engine) ReadPage(pageID uint64) ([]byte, error) {
	if e.db == nil {
		return nil, fmt.Errorf("engine: closed")
	}
	buf := make([]byte, PageSize)
	off, err := pageByteOffset(pageID)
	if err != nil {
		return nil, err
	}
	_, err = e.db.ReadAt(buf, off)
	if err != nil {
		return nil, err
	}
	return e.hooks.transformRead(pageID, buf)
}

// WritePage writes a full data page (pageID >= 1). Logs redo, fsyncs, updates header.
func (e *Engine) WritePage(pageID uint64, data []byte) error {
	if e.db == nil || e.wal == nil {
		return fmt.Errorf("engine: closed")
	}
	if pageID < 1 {
		return ErrBadPageID
	}
	if len(data) != PageSize {
		return ErrBadPageSize
	}
	plain, err := e.hooks.transformWrite(pageID, append([]byte(nil), data...))
	if err != nil {
		return err
	}
	if len(plain) != PageSize {
		return ErrBadPageSize
	}

	seq := e.lastSeq + 1
	rec := encodeWALRecordWithMAC(seq, pageID, plain, e.walMACKey, e.walMACEnabled)
	if _, err := e.wal.Write(rec); err != nil {
		return err
	}
	if err := e.wal.Sync(); err != nil {
		return err
	}

	off, err := pageByteOffset(pageID)
	if err != nil {
		return err
	}
	if _, err := e.db.WriteAt(plain, off); err != nil {
		return err
	}
	if err := e.db.Sync(); err != nil {
		return err
	}

	hdr, err := readHeaderAt(e.db)
	if err != nil {
		return err
	}
	hdr.LastWALSeq = seq
	if pageID+1 > hdr.NextPageID {
		hdr.NextPageID = pageID + 1
	}
	if err := writeHeaderAt(e.db, hdr); err != nil {
		return err
	}
	if err := e.db.Sync(); err != nil {
		return err
	}

	e.lastSeq = seq
	return nil
}

// Path returns the primary database path.
func (e *Engine) Path() string { return e.path }

// LastWALSeq returns the last applied redo sequence (for tests).
func (e *Engine) LastWALSeq() uint64 { return e.lastSeq }

// ReadHeader returns the current file header (page 0 prefix).
func (e *Engine) ReadHeader() (Header, error) {
	if e.db == nil {
		return Header{}, fmt.Errorf("engine: closed")
	}
	return readHeaderAt(e.db)
}

// UpdateHeader reads the header, applies mut, writes page 0, and fsyncs the DB file.
// Preserves all fields mut does not change (e.g. BTreeRoot vs WAL seq).
func (e *Engine) UpdateHeader(mut func(*Header)) error {
	if e.db == nil {
		return fmt.Errorf("engine: closed")
	}
	hdr, err := readHeaderAt(e.db)
	if err != nil {
		return err
	}
	mut(&hdr)
	if err := writeHeaderAt(e.db, hdr); err != nil {
		return err
	}
	return e.db.Sync()
}
