package hexxladb

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/hexxla/hexxladb/internal/index"
	"github.com/hexxla/hexxladb/internal/lattice"
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
	// the primary rows at the corresponding commit.
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

		if err := tx.AscendSeams(ctx, func(s SeamRecord) bool {
			report.SeamCount++
			if cfg.CheckOrphans {
				healthCheckOrphan(s, liveCells, &report)
			}
			if s.ResolutionStatus == "resolved" {
				report.SeamsResolved++
			} else {
				report.SeamsUnresolved++
			}
			return true
		}); err != nil {
			return fmt.Errorf("hexxladb: health seam scan: %w", err)
		}

		if cfg.CheckTagIndex {
			if err := healthCheckTagIndex(ctx, tx, liveCells, cfg.MaxErrors, &report); err != nil {
				return err
			}
		}

		if cfg.CheckSourceIndex {
			if err := healthCheckSourceIndex(ctx, tx, liveCells, cfg.MaxErrors, &report); err != nil {
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
	ch := db.cachedHdr.Load()
	stats.CommitSeq = ch.commitSeq

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
		prefixLen := len(index.CellPrefix) + index.PackedCoordKeyLen
		mvccLen := prefixLen + index.VersionSuffixLen
		if keyLen != mvccLen {
			scanErr = fmt.Errorf("%w: malformed cell primary key", ErrCorruptDatabase)
			return false
		}
		p, pErr := index.ParseCellKey(k[:prefixLen])
		if pErr != nil {
			scanErr = fmt.Errorf("%w: parse cell primary key: %w", ErrCorruptDatabase, pErr)
			return false
		}
		if _, unpackErr := lattice.Unpack(p); unpackErr != nil {
			scanErr = fmt.Errorf("%w: unpack cell primary coordinate: %w", ErrCorruptDatabase, unpackErr)
			return false
		}
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
		return true
	}); rangeErr != nil {
		return nil, 0, MVCCStats{}, fmt.Errorf("hexxladb: health cell scan: %w", rangeErr)
	}
	if scanErr != nil {
		return nil, 0, MVCCStats{}, scanErr
	}
	if err := flushMVCCCell(); err != nil {
		return nil, 0, MVCCStats{}, err
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

// healthCheckOrphan records a seam whose endpoints are malformed or missing.
func healthCheckOrphan(s record.SeamRecord, liveCells map[lattice.PackedCoord]struct{}, report *HealthReport) {
	aCoord, errA := lattice.Unpack(s.CellA)
	bCoord, errB := lattice.Unpack(s.CellB)
	if errA != nil || errB != nil {
		report.OrphanedSeams = append(report.OrphanedSeams, s.ID)
		report.Warnings = append(report.Warnings,
			fmt.Sprintf("seam %s: cannot unpack endpoint coords", s.ID))
		return
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

func healthCheckTagIndex(ctx context.Context, tx *Tx, liveCells map[lattice.PackedCoord]struct{}, maxErrors int, report *HealthReport) error {
	from, to := index.TagFamilyScanBounds()
	return healthCheckSecondary(ctx, tx, liveCells, from, to, maxErrors, &report.Warnings, &report.TagIndexErrors,
		"tag", index.ParseTagKeyWithSeq, func(rec record.CellRecord, id string) bool { return slices.Contains(rec.Tags, id) })
}

func healthCheckSourceIndex(ctx context.Context, tx *Tx, liveCells map[lattice.PackedCoord]struct{}, maxErrors int, report *HealthReport) error {
	return healthCheckSecondary(ctx, tx, liveCells, []byte(index.SourcePrefix), []byte("source0"), maxErrors, &report.Warnings, &report.SourceIndexErrors,
		"source", index.ParseSourceKeyWithSeq, func(rec record.CellRecord, id string) bool { return strings.TrimSpace(rec.Provenance.SourceID) == id })
}

func healthCheckSecondary(
	ctx context.Context, tx *Tx, live map[lattice.PackedCoord]struct{}, from, to []byte,
	maxErrors int, warnings *[]string, count *int, family string,
	parse func([]byte) (string, lattice.PackedCoord, uint64, bool, error),
	matches func(record.CellRecord, string) bool,
) error {
	var scanErr error
	if err := tx.AscendRange(from, to, func(k, _ []byte) bool {
		if scanErr = ctx.Err(); scanErr != nil {
			return false
		}
		id, coord, seq, versioned, err := parse(k)
		if err != nil {
			scanErr = fmt.Errorf("%w: parse %s secondary key: %w", ErrCorruptDatabase, family, err)
			return false
		}
		consistent, err := healthSecondaryConsistent(tx, live, family, id, coord, seq, versioned, matches)
		if err != nil {
			scanErr = err
			return false
		}
		if consistent {
			return true
		}
		*count++
		*warnings = append(*warnings, fmt.Sprintf("%s index: stale or inconsistent secondary for %s %q", family, family, id))
		if maxErrors > 0 && *count >= maxErrors {
			*warnings = append(*warnings, fmt.Sprintf("%s index check stopped at MaxErrors=%d", family, maxErrors))
			return false
		}
		return true
	}); err != nil {
		return fmt.Errorf("hexxladb: health %s index scan: %w", family, err)
	}
	return scanErr
}

func healthSecondaryConsistent(tx *Tx, live map[lattice.PackedCoord]struct{}, family, id string, coord lattice.PackedCoord, seq uint64, versioned bool, matches func(record.CellRecord, string) bool) (bool, error) {
	if !versioned {
		_, found := live[coord]
		return found, nil
	}
	val, found, err := tx.getDirect(index.CellKeyWithVersion(coord, seq))
	if err != nil || !found || len(val) == 0 {
		return false, err
	}
	rec, err := record.DecodeCell(val)
	if err != nil {
		return false, fmt.Errorf("%w: decode %s-indexed cell: %w", ErrCorruptDatabase, family, err)
	}
	if rec.Key != coord {
		return false, fmt.Errorf("%w: %s-indexed cell key mismatch", ErrCorruptDatabase, family)
	}
	return matches(rec, id), nil
}
