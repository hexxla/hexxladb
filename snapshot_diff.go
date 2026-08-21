package hexxladb

import (
	"bytes"
	"context"
	"fmt"

	"github.com/hexxla/hexxladb/internal/index"
	"github.com/hexxla/hexxladb/internal/lattice"
	"github.com/hexxla/hexxladb/internal/record"
)

// DiffOp indicates the nature of a change in a [SnapshotDiff].
type DiffOp string

const (
	// DiffOpPut means the cell or seam was written (created or updated) in the diff window.
	DiffOpPut DiffOp = "put"
	// DiffOpDelete means a cell tombstone was written in the diff window.
	DiffOpDelete DiffOp = "delete"
)

// CellDiff records a single cell write observed between two MVCC commit sequences.
type CellDiff struct {
	// Coord is the logical hex coordinate of the changed cell.
	Coord Coord
	// CommitSeq is the commit sequence at which this version was written.
	CommitSeq uint64
	// Op is [DiffOpPut] for a write or [DiffOpDelete] for a tombstone.
	Op DiffOp
	// Record is the decoded cell at this version.
	Record record.CellRecord
}

// SeamDiff records a single seam write observed between two MVCC commit sequences.
type SeamDiff struct {
	// ID is the ULID of the changed seam.
	ID string
	// CommitSeq is the commit sequence at which this version was written.
	CommitSeq uint64
	// Op is the operation type (currently always [DiffOpPut]).
	Op DiffOp
	// Record is the decoded seam at this version.
	Record record.SeamRecord
}

// SnapshotDiff holds all changes between two MVCC commit sequences.
// Changes are sorted by CommitSeq ascending within each slice.
type SnapshotDiff struct {
	FromSeq uint64
	ToSeq   uint64
	Cells   []CellDiff
	Seams   []SeamDiff
}

// SnapshotDiffConfig configures [DB.SnapshotDiff].
type SnapshotDiffConfig struct {
	// IncludeCells controls whether cell changes are collected. Default true when zero value.
	IncludeCells *bool
	// IncludeSeams controls whether seam changes are collected. Default true when zero value.
	IncludeSeams *bool
}

func diffInclude(p *bool) bool { return p == nil || *p }

// SnapshotDiff returns all cell and seam writes with commit_seq in the half-open range
// (fromSeq, toSeq]. Both sequences must be valid (≤ current CommitSeq).
// Requires an MVCC database (format v2); returns [ErrMVCCRequired] otherwise.
//
// The result is suitable for incremental replication, change-data-capture, and audit trails.
// Each entry carries the full decoded record at that commit version.
//
// Note: pruned versions (removed by [DB.PruneCellVersions]) will not appear in the diff.
func (db *DB) SnapshotDiff(ctx context.Context, fromSeq, toSeq uint64, cfg SnapshotDiffConfig) (SnapshotDiff, error) {
	if db == nil || db.closed.Load() {
		return SnapshotDiff{}, ErrDatabaseClosed
	}
	if err := ctx.Err(); err != nil {
		return SnapshotDiff{}, err
	}
	if fromSeq > toSeq {
		return SnapshotDiff{}, ErrInvalidArgument
	}

	db.mu.RLock()
	defer db.mu.RUnlock()
	if db.activeEng() == nil {
		return SnapshotDiff{}, ErrDatabaseClosed
	}
	if !db.useMVCC {
		return SnapshotDiff{}, ErrMVCCRequired
	}

	hdr, err := db.eng.ReadHeader()
	if err != nil {
		return SnapshotDiff{}, err
	}
	if toSeq > hdr.CommitSeq {
		return SnapshotDiff{}, ErrReadSeqFuture
	}

	diff := SnapshotDiff{FromSeq: fromSeq, ToSeq: toSeq}

	if diffInclude(cfg.IncludeCells) {
		cells, err := diffScanCells(ctx, db, fromSeq, toSeq)
		if err != nil {
			return SnapshotDiff{}, err
		}
		diff.Cells = cells
	}

	if diffInclude(cfg.IncludeSeams) {
		seams, err := diffScanSeams(ctx, db, fromSeq, toSeq)
		if err != nil {
			return SnapshotDiff{}, err
		}
		diff.Seams = seams
	}

	return diff, nil
}

// diffScanCells scans cell/<packed><seq8> MVCC keys for versions in (fromSeq, toSeq].
func diffScanCells(ctx context.Context, db *DB, fromSeq, toSeq uint64) ([]CellDiff, error) {
	from := []byte(index.CellPrefix)
	to := append([]byte(index.CellPrefix), 0xff)
	var out []CellDiff
	var scanErr error

	err := db.btree.AscendRange(from, to, func(k, v []byte) bool {
		if err := ctx.Err(); err != nil {
			scanErr = err
			return false
		}
		if !bytes.HasPrefix(k, []byte(index.CellPrefix)) {
			return false
		}
		// Only MVCC keys have the version suffix (len = CellPrefix + 16 packed + 8 seq)
		packed, seq, err := index.ParseCellVersionKey(k)
		if err != nil {
			scanErr = fmt.Errorf("%w: invalid cell version key: %w", ErrCorruptDatabase, err)
			return false
		}
		if seq <= fromSeq || seq > toSeq {
			return true
		}
		coord, err := lattice.Unpack(packed)
		if err != nil {
			scanErr = fmt.Errorf("%w: invalid packed cell coordinate: %w", ErrCorruptDatabase, err)
			return false
		}
		if len(v) == 0 {
			out = append(out, CellDiff{Coord: coord, CommitSeq: seq, Op: DiffOpDelete})
			return true
		}
		rec, err := record.DecodeCell(v)
		if err != nil {
			scanErr = fmt.Errorf("%w: decode cell version: %w", ErrCorruptDatabase, err)
			return false
		}
		out = append(out, CellDiff{
			Coord:     coord,
			CommitSeq: seq,
			Op:        DiffOpPut,
			Record:    rec,
		})
		return true
	})
	if err != nil {
		return nil, err
	}
	return out, scanErr
}

// diffScanSeams scans seam/<ulid>/<seq8> MVCC keys for versions in (fromSeq, toSeq].
func diffScanSeams(ctx context.Context, db *DB, fromSeq, toSeq uint64) ([]SeamDiff, error) {
	// MVCC seam version keys: seam/<ulid>/<seq8>
	// Non-version seam primary keys: seam/<ulid>  (shorter — different length, ParseSeamVersionKey will reject)
	from := []byte(index.SeamPrefix)
	to := index.SeamScanUpperBound()
	var out []SeamDiff
	var scanErr error

	err := db.btree.AscendRange(from, to, func(k, v []byte) bool {
		if err := ctx.Err(); err != nil {
			scanErr = err
			return false
		}
		ulidStr, seq, err := index.ParseSeamVersionKey(k)
		if err != nil {
			return true // primary (non-versioned) seam key or secondary — skip
		}
		if seq <= fromSeq || seq > toSeq {
			return true
		}
		if len(v) == 0 {
			return true // seam-by-cells index key has nil value — skip
		}
		rec, err := record.DecodeSeam(v)
		if err != nil {
			scanErr = fmt.Errorf("%w: decode seam version: %w", ErrCorruptDatabase, err)
			return false
		}
		out = append(out, SeamDiff{
			ID:        ulidStr,
			CommitSeq: seq,
			Op:        DiffOpPut,
			Record:    rec,
		})
		return true
	})
	if err != nil {
		return nil, err
	}
	return out, scanErr
}
