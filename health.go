package hexxladb

import (
	"bytes"
	"context"
	"fmt"
	"slices"

	"github.com/hexxla/hexxladb/internal/index"
	"github.com/hexxla/hexxladb/internal/lattice"
	"github.com/hexxla/hexxladb/internal/mvcc"
	"github.com/hexxla/hexxladb/internal/record"
)

// HealthReport summarises the result of a [DB.HealthCheck] run.
// All counts reflect the MVCC-visible snapshot at the moment of the check.
type HealthReport struct {
	// CellCount is the number of visible cells found by scanning the cell/ primary key range.
	CellCount int
	// SeamCount is the number of visible seams found.
	SeamCount int
	// SeamsResolved / SeamsUnresolved partition SeamCount by ResolutionStatus.
	SeamsResolved   int
	SeamsUnresolved int

	// OrphanedSeams lists ULID IDs of seams whose CellA or CellB no longer
	// exist as visible cells. Populated only when HealthCheckConfig.CheckOrphans is true.
	OrphanedSeams []string

	// TagIndexErrors is the count of tag/ secondary index entries inconsistent with
	// the primary cell rows they refer to (missing or tombstoned version, or decoded
	// cell does not carry the tag). Populated only when CheckTagIndex is true.
	TagIndexErrors int
	// SourceIndexErrors is the count of source/ index entries inconsistent with
	// the primary rows at the corresponding commit (or, for v1, no visible cell at coord).
	SourceIndexErrors int

	// MVCCStats is a snapshot of MVCC counters at check time.
	MVCCStats MVCCStats

	// Warnings contains human-readable diagnostic messages (non-fatal anomalies).
	Warnings []string
}

// HealthCheckConfig controls optional cross-record checks performed by [DB.HealthCheck].
// Its zero value disables the optional checks; primary cell and seam decoding is
// always performed. Use [DefaultHealthCheckConfig] to enable every optional check.
type HealthCheckConfig struct {
	// CheckOrphans verifies that every seam's endpoints exist as visible cells.
	CheckOrphans bool
	// CheckTagIndex verifies every tag/ secondary index entry has a live primary cell.
	CheckTagIndex bool
	// CheckSourceIndex verifies every source/ secondary index entry has a live primary cell.
	CheckSourceIndex bool
	// MaxErrors stops counting secondary-index errors after this many (0 = unlimited).
	MaxErrors int
}

// DefaultHealthCheckConfig returns a HealthCheckConfig with all checks enabled
// and conservative defaults.
func DefaultHealthCheckConfig() HealthCheckConfig {
	return HealthCheckConfig{
		CheckOrphans:     true,
		CheckTagIndex:    true,
		CheckSourceIndex: true,
		MaxErrors:        0,
	}
}

// HealthCheck performs an integrity scan of the database and returns a [HealthReport].
// It runs inside a stable read-only snapshot. Other readers may proceed, but the
// snapshot blocks writers until the scan completes. ctx cancellation is respected
// between records.
func (db *DB) HealthCheck(ctx context.Context, cfg HealthCheckConfig) (HealthReport, error) {
	if db == nil {
		return HealthReport{}, ErrClosed
	}
	if cfg.MaxErrors < 0 {
		return HealthReport{}, ErrInvalidArgument
	}
	var report HealthReport

	if err := db.View(func(tx *Tx) error {
		if err := ctx.Err(); err != nil {
			return err
		}

		liveCells, cellCount, mvccStats, err := healthScanCells(ctx, tx, db)
		if err != nil {
			return err
		}
		report.CellCount = cellCount
		report.MVCCStats = mvccStats

		seams, err := healthScanSeams(ctx, tx, db.useMVCC)
		if err != nil {
			return err
		}
		report.SeamCount = len(seams)
		for _, s := range seams {
			if s.ResolutionStatus == "resolved" {
				report.SeamsResolved++
			} else {
				report.SeamsUnresolved++
			}
		}

		if cfg.CheckOrphans {
			if err := healthCheckOrphans(ctx, seams, liveCells, &report); err != nil {
				return err
			}
		}

		if cfg.CheckTagIndex {
			if err := healthCheckTagIndex(ctx, tx, db.useMVCC, liveCells, cfg.MaxErrors, &report); err != nil {
				return err
			}
		}

		if cfg.CheckSourceIndex {
			if err := healthCheckSourceIndex(ctx, tx, db.useMVCC, liveCells, cfg.MaxErrors, &report); err != nil {
				return err
			}
		}

		return nil
	}); err != nil {
		return HealthReport{}, err
	}

	return report, nil
}

// healthScanCells scans the cell/ primary key range and returns the set of
// live PackedCoords, the total live cell count, and MVCC stats computed in the
// same pass — eliminating the separate StatsMVCC() scan that HealthCheck previously issued.
func healthScanCells(ctx context.Context, tx *Tx, db *DB) (liveCells map[lattice.PackedCoord]struct{}, cellCount int, stats MVCCStats, err error) {
	isMVCC := db.useMVCC
	ch := db.cachedHdr.Load()
	stats.CommitSeq = ch.commitSeq
	stats.WastedBytes = db.eng.WastedBytes()

	liveCells = map[lattice.PackedCoord]struct{}{}
	cellFrom := []byte(index.CellPrefix)
	cellTo := []byte("cell0") // sorts after all cell/<16-byte packed> keys
	var scanErr error

	var lastCoord lattice.PackedCoord
	var lastValue []byte
	var hasPrevCoord bool
	flushMVCCCell := func() error {
		if !hasPrevCoord {
			return nil
		}
		stats.LogicalCells++
		if len(lastValue) == 0 {
			return nil
		}
		if err := validateHealthCell(lastCoord, lastValue); err != nil {
			return err
		}
		liveCells[lastCoord] = struct{}{}
		cellCount++
		return nil
	}

	if rangeErr := tx.AscendRange(cellFrom, cellTo, func(k, v []byte) bool {
		if ctxErr := ctx.Err(); ctxErr != nil {
			scanErr = ctxErr
			return false
		}
		keyLen := len(k)
		v1Len := len(index.CellPrefix) + index.PackedCoordKeyLen
		mvccLen := v1Len + index.VersionSuffixLen
		wantLen := v1Len
		if isMVCC {
			wantLen = mvccLen
		}
		if keyLen != wantLen {
			scanErr = fmt.Errorf("%w: malformed cell primary key", ErrCorruptDatabase)
			return false
		}
		p, pErr := index.ParseCellKey(k[:v1Len])
		if pErr != nil {
			scanErr = fmt.Errorf("%w: parse cell primary key: %w", ErrCorruptDatabase, pErr)
			return false
		}
		if _, unpackErr := lattice.Unpack(p); unpackErr != nil {
			scanErr = fmt.Errorf("%w: unpack cell primary coordinate: %w", ErrCorruptDatabase, unpackErr)
			return false
		}
		if isMVCC {
			stats.VersionedRows++
			if !hasPrevCoord || p != lastCoord {
				if err := flushMVCCCell(); err != nil {
					scanErr = err
					return false
				}
				lastCoord = p
				hasPrevCoord = true
			}
			lastValue = slices.Clone(v)
		} else {
			if len(v) == 0 {
				scanErr = fmt.Errorf("%w: empty non-MVCC cell record", ErrCorruptDatabase)
				return false
			}
			if err := validateHealthCell(p, v); err != nil {
				scanErr = err
				return false
			}
			liveCells[p] = struct{}{}
			cellCount++
		}
		return true
	}); rangeErr != nil {
		return nil, 0, MVCCStats{}, fmt.Errorf("hexxladb: health cell scan: %w", rangeErr)
	}
	if scanErr != nil {
		return nil, 0, MVCCStats{}, scanErr
	}
	if isMVCC {
		if err := flushMVCCCell(); err != nil {
			return nil, 0, MVCCStats{}, err
		}
	}
	return liveCells, cellCount, stats, nil
}

func validateHealthCell(key lattice.PackedCoord, value []byte) error {
	rec, err := record.DecodeCell(value)
	if err != nil {
		return fmt.Errorf("%w: decode visible cell: %w", ErrCorruptDatabase, err)
	}
	if rec.Key != key {
		return fmt.Errorf("%w: cell primary key does not match encoded record key", ErrCorruptDatabase)
	}
	if err := validateCellInvariants(rec); err != nil {
		return fmt.Errorf("%w: invalid visible cell: %w", ErrCorruptDatabase, err)
	}
	return nil
}

// healthScanSeams scans seam/ primary keys and returns decoded visible seams.
func healthScanSeams(ctx context.Context, tx *Tx, isMVCC bool) ([]record.SeamRecord, error) {
	var seams []record.SeamRecord
	var scanErr error

	if isMVCC {
		seams, scanErr = healthScanSeamsMVCC(ctx, tx)
	} else {
		seams, scanErr = healthScanSeamsV1(ctx, tx)
	}
	return seams, scanErr
}

// healthScanSeamsMVCC scans seams with MVCC version grouping.
func healthScanSeamsMVCC(ctx context.Context, tx *Tx) ([]record.SeamRecord, error) {
	var seams []record.SeamRecord
	var grouped []mvcc.VersionKV
	var curULID string
	var scanErr error

	flushGrouped := func() error {
		if len(grouped) == 0 {
			return nil
		}
		val, _, ok := mvcc.SelectVisible(grouped, tx.readSeq)
		grouped = grouped[:0]
		if !ok || len(val) == 0 {
			return nil
		}
		s, err := record.DecodeSeam(val)
		if err != nil {
			return fmt.Errorf("%w: decode visible seam %q: %w", ErrCorruptDatabase, curULID, err)
		}
		if s.ID != curULID {
			return fmt.Errorf("%w: seam primary key does not match encoded record ID", ErrCorruptDatabase)
		}
		if err := validateSeamInvariants(s); err != nil {
			return fmt.Errorf("%w: invalid visible seam: %w", ErrCorruptDatabase, err)
		}
		seams = append(seams, s)
		return nil
	}

	if err := tx.AscendRange(
		[]byte(index.SeamPrefix),
		index.SeamScanUpperBound(),
		func(k, v []byte) bool {
			if err := ctx.Err(); err != nil {
				scanErr = err
				return false
			}
			ulidStr, seq, err := index.ParseSeamVersionKey(k)
			if err != nil {
				scanErr = fmt.Errorf("%w: parse seam primary key: %w", ErrCorruptDatabase, err)
				return false
			}
			if ulidStr != curULID {
				if err := flushGrouped(); err != nil {
					scanErr = err
					return false
				}
				curULID = ulidStr
			}
			grouped = append(grouped, mvcc.VersionKV{
				CommitSeq: seq,
				Value:     slices.Clone(v),
			})
			return true
		},
	); err != nil {
		return nil, fmt.Errorf("hexxladb: health seam scan: %w", err)
	}
	if scanErr != nil {
		return nil, scanErr
	}
	if err := flushGrouped(); err != nil {
		return nil, err
	}
	return seams, nil
}

// healthScanSeamsV1 scans seams in v1 format (one key per seam ULID).
func healthScanSeamsV1(ctx context.Context, tx *Tx) ([]record.SeamRecord, error) {
	var seams []record.SeamRecord
	var scanErr error

	if err := tx.AscendRange(
		[]byte(index.SeamPrefix),
		index.SeamScanUpperBound(),
		func(k, v []byte) bool {
			if err := ctx.Err(); err != nil {
				scanErr = err
				return false
			}
			s, err := record.DecodeSeam(v)
			if err != nil {
				scanErr = fmt.Errorf("%w: decode visible seam: %w", ErrCorruptDatabase, err)
				return false
			}
			expectedKey, err := index.SeamKey(s.ID)
			if err != nil || !bytes.Equal(k, expectedKey) {
				scanErr = fmt.Errorf("%w: seam primary key does not match encoded record ID", ErrCorruptDatabase)
				return false
			}
			if err := validateSeamInvariants(s); err != nil {
				scanErr = fmt.Errorf("%w: invalid visible seam: %w", ErrCorruptDatabase, err)
				return false
			}
			seams = append(seams, s)
			return true
		},
	); err != nil {
		return nil, fmt.Errorf("hexxladb: health seam scan: %w", err)
	}
	if scanErr != nil {
		return nil, scanErr
	}
	return seams, nil
}

// healthCheckOrphans detects seams whose CellA or CellB are not in liveCells.
func healthCheckOrphans(ctx context.Context, seams []record.SeamRecord, liveCells map[lattice.PackedCoord]struct{}, report *HealthReport) error {
	for _, s := range seams {
		if err := ctx.Err(); err != nil {
			return err
		}
		aCoord, errA := lattice.Unpack(s.CellA)
		bCoord, errB := lattice.Unpack(s.CellB)
		if errA != nil || errB != nil {
			report.OrphanedSeams = append(report.OrphanedSeams, s.ID)
			report.Warnings = append(report.Warnings,
				fmt.Sprintf("seam %s: cannot unpack endpoint coords", s.ID))
			continue
		}
		_, aOk := liveCells[s.CellA]
		_, bOk := liveCells[s.CellB]
		if !aOk || !bOk {
			report.OrphanedSeams = append(report.OrphanedSeams, s.ID)
			report.Warnings = append(report.Warnings, fmt.Sprintf(
				"seam %s: endpoint (%d,%d) or (%d,%d) has no visible cell",
				s.ID, aCoord.Q, aCoord.R, bCoord.Q, bCoord.R,
			))
		}
	}
	return nil
}

// healthCheckTagIndex verifies every tag/ secondary index entry has a live primary cell.
func healthCheckTagIndex(ctx context.Context, tx *Tx, isMVCC bool, liveCells map[lattice.PackedCoord]struct{}, maxErrors int, report *HealthReport) error {
	tagFrom, tagTo := index.TagFamilyScanBounds()
	var scanErr error

	if err := tx.AscendRange(tagFrom, tagTo, func(k, _ []byte) bool {
		if err := ctx.Err(); err != nil {
			scanErr = err
			return false
		}
		tagAlive, terr := secondaryTagIndexConsistent(tx, isMVCC, k, liveCells)
		if terr != nil {
			scanErr = terr
			return false
		}
		if !tagAlive {
			tag, _, _, _, parseErr := index.ParseTagKeyWithSeq(k)
			if parseErr != nil {
				return true
			}
			report.TagIndexErrors++
			report.Warnings = append(report.Warnings, fmt.Sprintf(
				"tag index: stale or inconsistent secondary for tag %q", tag,
			))
			if maxErrors > 0 && report.TagIndexErrors >= maxErrors {
				report.Warnings = append(report.Warnings,
					fmt.Sprintf("tag index check stopped at MaxErrors=%d", maxErrors))
				return false
			}
		}
		return true
	}); err != nil {
		return fmt.Errorf("hexxladb: health tag index scan: %w", err)
	}
	return scanErr
}

// healthCheckSourceIndex verifies every source/ secondary index entry has a live primary cell.
func healthCheckSourceIndex(ctx context.Context, tx *Tx, isMVCC bool, liveCells map[lattice.PackedCoord]struct{}, maxErrors int, report *HealthReport) error {
	var scanErr error

	if err := tx.AscendRange(
		[]byte(index.SourcePrefix),
		[]byte("source0"),
		func(k, _ []byte) bool {
			if err := ctx.Err(); err != nil {
				scanErr = err
				return false
			}
			srcAlive, sErr := secondarySourceIndexConsistent(tx, isMVCC, k, liveCells)
			if sErr != nil {
				scanErr = sErr
				return false
			}
			if !srcAlive {
				sid, _, _, _, parseErr := index.ParseSourceKeyWithSeq(k)
				if parseErr != nil {
					return true
				}
				report.SourceIndexErrors++
				report.Warnings = append(report.Warnings, fmt.Sprintf(
					"source index: stale or inconsistent secondary for source %q", sid,
				))
				if maxErrors > 0 && report.SourceIndexErrors >= maxErrors {
					report.Warnings = append(report.Warnings,
						fmt.Sprintf("source index check stopped at MaxErrors=%d", maxErrors))
					return false
				}
			}
			return true
		},
	); err != nil {
		return fmt.Errorf("hexxladb: health source index scan: %w", err)
	}
	return scanErr
}

// secondaryTagIndexConsistent verifies a physical tag/ key against primary storage.
// MVCC suffixed keys are validated against cell/<packed><seq>; v1 keys and
// unsuffixed tag keys check live head cells only. Malformed storage fails closed.
func secondaryTagIndexConsistent(tx *Tx, dbMVCC bool, k []byte, live map[lattice.PackedCoord]struct{}) (bool, error) {
	tag, p, seq, hasSeq, err := index.ParseTagKeyWithSeq(k)
	if err != nil {
		return false, fmt.Errorf("%w: parse tag secondary key: %w", ErrCorruptDatabase, err)
	}
	if dbMVCC && hasSeq {
		ck := index.CellKeyWithVersion(p, seq)
		val, found, err := tx.getDirect(ck)
		if err != nil {
			return false, err
		}
		if !found || len(val) == 0 {
			return false, nil
		}
		rec, err := record.DecodeCell(val)
		if err != nil {
			return false, fmt.Errorf("%w: decode tag-indexed cell: %w", ErrCorruptDatabase, err)
		}
		if rec.Key != p {
			return false, fmt.Errorf("%w: tag-indexed cell key mismatch", ErrCorruptDatabase)
		}
		return slices.Contains(rec.Tags, tag), nil
	}
	_, alive := live[p]
	return alive, nil
}

func secondarySourceIndexConsistent(tx *Tx, dbMVCC bool, k []byte, live map[lattice.PackedCoord]struct{}) (bool, error) {
	sid, p, seq, hasSeq, err := index.ParseSourceKeyWithSeq(k)
	if err != nil {
		return false, fmt.Errorf("%w: parse source secondary key: %w", ErrCorruptDatabase, err)
	}
	if dbMVCC && hasSeq {
		ck := index.CellKeyWithVersion(p, seq)
		val, found, err := tx.getDirect(ck)
		if err != nil {
			return false, err
		}
		if !found || len(val) == 0 {
			return false, nil
		}
		rec, err := record.DecodeCell(val)
		if err != nil {
			return false, fmt.Errorf("%w: decode source-indexed cell: %w", ErrCorruptDatabase, err)
		}
		if rec.Key != p {
			return false, fmt.Errorf("%w: source-indexed cell key mismatch", ErrCorruptDatabase)
		}
		return rec.Provenance.SourceID == sid, nil
	}
	_, alive := live[p]
	return alive, nil
}
