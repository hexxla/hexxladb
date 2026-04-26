package hexxladb

import (
	"context"
	"fmt"

	"github.com/hexxla/hexxladb/internal/lattice"
	"github.com/hexxla/hexxladb/internal/record"
)

// HealthReport summarises the result of a [DB.HealthCheck] run.
// All counts reflect the MVCC-visible snapshot at the moment of the check.
type HealthReport struct {
	// CellCount is the number of visible cells found by walking rings from origin.
	CellCount int
	// SeamCount is the number of visible seams found.
	SeamCount int
	// SeamsResolved / SeamsUnresolved partition SeamCount by ResolutionStatus.
	SeamsResolved   int
	SeamsUnresolved int

	// OrphanedSeams lists ULID IDs of seams whose CellA or CellB no longer
	// exist as visible cells. Populated only when HealthCheckConfig.CheckOrphans is true.
	OrphanedSeams []string

	// TagIndexErrors is the count of tag/ secondary index entries that point to a
	// coord with no visible primary cell. Populated only when CheckTagIndex is true.
	TagIndexErrors int
	// SourceIndexErrors is the count of source/ index entries with no visible cell.
	SourceIndexErrors int

	// MVCCStats is a snapshot of MVCC counters at check time.
	MVCCStats MVCCStats

	// Warnings contains human-readable diagnostic messages (non-fatal anomalies).
	Warnings []string
}

// HealthCheckConfig controls which checks [DB.HealthCheck] performs.
// All boolean fields default to true; set explicitly to false to skip a check.
type HealthCheckConfig struct {
	// CheckOrphans verifies that every seam's endpoints exist as visible cells.
	CheckOrphans bool
	// CheckTagIndex verifies every tag/ secondary index entry has a live primary cell.
	CheckTagIndex bool
	// CheckSourceIndex verifies every source/ secondary index entry has a live primary cell.
	CheckSourceIndex bool
	// MaxErrors stops counting secondary-index errors after this many (0 = unlimited).
	MaxErrors int
	// ScanRadius controls how far from origin to walk when counting cells (default 64).
	// Increase for sparse or geographically wide databases.
	ScanRadius int
}

// DefaultHealthCheckConfig returns a HealthCheckConfig with all checks enabled
// and conservative defaults.
func DefaultHealthCheckConfig() HealthCheckConfig {
	return HealthCheckConfig{
		CheckOrphans:     true,
		CheckTagIndex:    true,
		CheckSourceIndex: true,
		MaxErrors:        0,
		ScanRadius:       64,
	}
}

// HealthCheck performs an integrity scan of the database and returns a [HealthReport].
// It runs inside a read-only snapshot so it does not block concurrent writes.
// ctx cancellation is respected between cells.
func (db *DB) HealthCheck(ctx context.Context, cfg HealthCheckConfig) (HealthReport, error) {
	if db == nil {
		return HealthReport{}, ErrClosed
	}
	if cfg.ScanRadius <= 0 {
		cfg.ScanRadius = 64
	}

	var report HealthReport
	var err error

	report.MVCCStats, err = db.StatsMVCC()
	if err != nil {
		return HealthReport{}, fmt.Errorf("hexxladb: health check StatsMVCC: %w", err)
	}

	if err := db.View(func(tx *Tx) error {
		if err := ctx.Err(); err != nil {
			return err
		}

		// ── cell count ───────────────────────────────────────────────────────
		coords := WalkRings(nil, Coord{}, cfg.ScanRadius)
		for _, c := range coords {
			if err := ctx.Err(); err != nil {
				return err
			}
			_, ok, err := tx.GetCell(mustPack(c))
			if err != nil {
				return fmt.Errorf("hexxladb: health GetCell at (%d,%d): %w", c.Q, c.R, err)
			}
			if ok {
				report.CellCount++
			}
		}

		// ── seam count + resolution summary ──────────────────────────────────
		seams, err := tx.FindSeams(ctx, Coord{}, cfg.ScanRadius, false)
		if err != nil {
			return fmt.Errorf("hexxladb: health FindSeams: %w", err)
		}
		report.SeamCount = len(seams)
		for _, s := range seams {
			if s.ResolutionStatus == "resolved" {
				report.SeamsResolved++
			} else {
				report.SeamsUnresolved++
			}
		}

		// ── orphaned seam detection ───────────────────────────────────────────
		if cfg.CheckOrphans {
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
				_, aOk, err := tx.GetCell(s.CellA)
				if err != nil {
					return fmt.Errorf("hexxladb: health orphan check CellA: %w", err)
				}
				_, bOk, err := tx.GetCell(s.CellB)
				if err != nil {
					return fmt.Errorf("hexxladb: health orphan check CellB: %w", err)
				}
				if !aOk || !bOk {
					report.OrphanedSeams = append(report.OrphanedSeams, s.ID)
					report.Warnings = append(report.Warnings, fmt.Sprintf(
						"seam %s: endpoint (%d,%d) or (%d,%d) has no visible cell",
						s.ID, aCoord.Q, aCoord.R, bCoord.Q, bCoord.R,
					))
				}
			}
		}

		// ── tag index consistency ─────────────────────────────────────────────
		if cfg.CheckTagIndex {
			tags, err := tx.ListExistingTopics(ctx)
			if err != nil {
				return fmt.Errorf("hexxladb: health ListExistingTopics: %w", err)
			}
			errLimit := cfg.MaxErrors
			tagDone := false
			for _, tag := range tags {
				if tagDone {
					break
				}
				if err := ctx.Err(); err != nil {
					return err
				}
				if err := tx.AscendCellsByTag(ctx, tag, func(rec record.CellRecord) bool {
					_, ok, e := tx.GetCell(rec.Key)
					if e != nil || !ok {
						report.TagIndexErrors++
						report.Warnings = append(report.Warnings, fmt.Sprintf(
							"tag index: tag %q points to coord with no visible cell", tag,
						))
						if errLimit > 0 && report.TagIndexErrors >= errLimit {
							return false
						}
					}
					return true
				}); err != nil {
					return fmt.Errorf("hexxladb: health AscendCellsByTag %q: %w", tag, err)
				}
				if errLimit > 0 && report.TagIndexErrors >= errLimit {
					report.Warnings = append(report.Warnings,
						fmt.Sprintf("tag index check stopped at MaxErrors=%d", errLimit))
					tagDone = true
				}
			}
		}

		// ── source index consistency ──────────────────────────────────────────
		if cfg.CheckSourceIndex {
			errLimit := cfg.MaxErrors
			// Enumerate distinct source IDs from the cells already counted above.
			sourceIDs := map[string]struct{}{}
			for _, c := range coords {
				rec, ok, err := tx.GetCell(mustPack(c))
				if err != nil {
					return err
				}
				if ok && rec.Provenance.SourceID != "" {
					sourceIDs[rec.Provenance.SourceID] = struct{}{}
				}
			}
			srcDone := false
			for sid := range sourceIDs {
				if srcDone {
					break
				}
				if err := ctx.Err(); err != nil {
					return err
				}
				if err := tx.AscendCellsBySource(ctx, sid, func(rec record.CellRecord) bool {
					_, ok, e := tx.GetCell(rec.Key)
					if e != nil || !ok {
						report.SourceIndexErrors++
						report.Warnings = append(report.Warnings, fmt.Sprintf(
							"source index: source %q points to coord with no visible cell", sid,
						))
						if errLimit > 0 && report.SourceIndexErrors >= errLimit {
							return false
						}
					}
					return true
				}); err != nil {
					return fmt.Errorf("hexxladb: health AscendCellsBySource %q: %w", sid, err)
				}
				if errLimit > 0 && report.SourceIndexErrors >= errLimit {
					report.Warnings = append(report.Warnings,
						fmt.Sprintf("source index check stopped at MaxErrors=%d", errLimit))
					srcDone = true
				}
			}
		}

		return nil
	}); err != nil {
		return HealthReport{}, err
	}

	return report, nil
}
