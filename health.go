package hexxladb

import (
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
// It runs inside a read-only snapshot so it does not block concurrent writes.
// ctx cancellation is respected between cells.
func (db *DB) HealthCheck(ctx context.Context, cfg HealthCheckConfig) (HealthReport, error) {
	if db == nil {
		return HealthReport{}, ErrClosed
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

		// ── cell count + source ID collection (single forward pass) ───────────
		// Scan the cell/ primary key range directly: visits only existing cells
		// in O(n) regardless of coordinate sparsity, with no radius limit.
		var cellScanErr error
		// liveCells holds every PackedCoord with a visible primary cell;
		// used for O(1) presence checks in tag/source/orphan verification below.
		liveCells := map[lattice.PackedCoord]struct{}{}
		cellFrom := []byte(index.CellPrefix)
		cellTo := []byte("cell0") // sorts after all cell/<16-byte packed> keys
		isMVCC := db.useMVCC
		// For MVCC databases, keys ascend as (packed_coord, commit_seq).
		// The last entry per coord carries the latest version — if its value
		// is zero-length the cell was tombstoned by DeleteCell and must not
		// be counted as live.
		var lastCoord lattice.PackedCoord
		var lastCoordLive bool // true if latest version for lastCoord has len(v) > 0
		if err := tx.AscendRange(cellFrom, cellTo, func(k, v []byte) bool {
			if err := ctx.Err(); err != nil {
				cellScanErr = err
				return false
			}
			// Accept both v1 keys (cell/<16>) and MVCC version keys (cell/<16><8-byte seq>).
			keyLen := len(k)
			v1Len := len(index.CellPrefix) + index.PackedCoordKeyLen
			mvccLen := v1Len + index.VersionSuffixLen
			if keyLen != v1Len && keyLen != mvccLen {
				return true
			}
			p, err := index.ParseCellKey(k[:v1Len])
			if err != nil {
				return true
			}
			if isMVCC {
				// Keys ascend by (coord, seq). When we move to a new coord,
				// finalize the previous one.
				if p != lastCoord {
					if lastCoordLive {
						liveCells[lastCoord] = struct{}{}
						report.CellCount++
					}
					lastCoord = p
					lastCoordLive = false
				}
				// Each successive key for the same coord has a higher seq;
				// overwrite so at the end of iteration lastCoordLive reflects
				// the latest version.
				lastCoordLive = len(v) > 0
			} else {
				liveCells[p] = struct{}{}
				report.CellCount++
			}
			return true
		}); err != nil {
			return fmt.Errorf("hexxladb: health cell scan: %w", err)
		}
		// Finalize the last MVCC coord after the scan completes.
		if isMVCC && lastCoordLive {
			liveCells[lastCoord] = struct{}{}
			report.CellCount++
		}
		if cellScanErr != nil {
			return cellScanErr
		}

		// ── seam count + resolution summary (seam/ primary scan) ───────────
		// Format v1: one primary key per seam ULID. MVCC: one key per write
		// ([SeamKeyWithVersion]); same logical seam shares a ULID prefix and must be
		// collapsed via [mvcc.SelectVisible] — otherwise PutSeam + ResolveSeam double-counts.
		var seams []record.SeamRecord
		var seamScanErr error
		if isMVCC {
			var grouped []mvcc.VersionKV
			var curULID string
			flushGrouped := func() {
				if len(grouped) == 0 {
					return
				}
				val, _, ok := mvcc.SelectVisible(grouped, tx.readSeq)
				grouped = grouped[:0]
				if !ok || len(val) == 0 {
					return
				}
				s, err := record.DecodeSeam(val)
				if err != nil {
					return
				}
				seams = append(seams, s)
			}
			if err := tx.AscendRange(
				[]byte(index.SeamPrefix),
				index.SeamScanUpperBound(),
				func(k, v []byte) bool {
					if err := ctx.Err(); err != nil {
						seamScanErr = err
						return false
					}
					ulidStr, seq, err := index.ParseSeamVersionKey(k)
					if err != nil {
						return true // malformed layout — skip like corrupt v1 decode
					}
					if ulidStr != curULID {
						flushGrouped()
						curULID = ulidStr
					}
					grouped = append(grouped, mvcc.VersionKV{
						CommitSeq: seq,
						Value:     slices.Clone(v),
					})
					return true
				},
			); err != nil {
				return fmt.Errorf("hexxladb: health seam scan: %w", err)
			}
			if seamScanErr != nil {
				return seamScanErr
			}
			flushGrouped()
		} else if err := tx.AscendRange(
			[]byte(index.SeamPrefix),
			index.SeamScanUpperBound(),
			func(_, v []byte) bool {
				if err := ctx.Err(); err != nil {
					seamScanErr = err
					return false
				}
				s, err := record.DecodeSeam(v)
				if err != nil {
					return true // corrupt entry — skip
				}
				seams = append(seams, s)
				return true
			},
		); err != nil {
			return fmt.Errorf("hexxladb: health seam scan: %w", err)
		} else if seamScanErr != nil {
			return seamScanErr
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
		}

		// ── tag index consistency (single tag/ prefix scan) ──────────────────
		if cfg.CheckTagIndex {
			errLimit := cfg.MaxErrors
			tagFrom, tagTo := index.TagFamilyScanBounds()
			var tagScanErr error
			if err := tx.AscendRange(tagFrom, tagTo, func(k, _ []byte) bool {
				if err := ctx.Err(); err != nil {
					tagScanErr = err
					return false
				}
				tagAlive, terr := secondaryTagIndexConsistent(tx, isMVCC, k, liveCells)
				if terr != nil {
					tagScanErr = terr
					return false
				}
				if !tagAlive {
					tag, _, _, _, parseErr := index.ParseTagKeyWithSeq(k)
					if parseErr != nil {
						return true // malformed — skip without counting
					}
					report.TagIndexErrors++
					report.Warnings = append(report.Warnings, fmt.Sprintf(
						"tag index: stale or inconsistent secondary for tag %q", tag,
					))
					if errLimit > 0 && report.TagIndexErrors >= errLimit {
						report.Warnings = append(report.Warnings,
							fmt.Sprintf("tag index check stopped at MaxErrors=%d", errLimit))
						return false
					}
				}
				return true
			}); err != nil {
				return fmt.Errorf("hexxladb: health tag index scan: %w", err)
			}
			if tagScanErr != nil {
				return tagScanErr
			}
		}

		// ── source index consistency (single source/ prefix scan) ────────────
		if cfg.CheckSourceIndex {
			errLimit := cfg.MaxErrors
			var srcScanErr error
			if err := tx.AscendRange(
				[]byte(index.SourcePrefix),
				[]byte("source0"), // sorts after all source/<len><id>/... keys
				func(k, _ []byte) bool {
					if err := ctx.Err(); err != nil {
						srcScanErr = err
						return false
					}
					srcAlive, sErr := secondarySourceIndexConsistent(tx, isMVCC, k, liveCells)
					if sErr != nil {
						srcScanErr = sErr
						return false
					}
					if !srcAlive {
						sid, _, _, _, parseErr := index.ParseSourceKeyWithSeq(k)
						if parseErr != nil {
							return true // malformed — skip
						}
						report.SourceIndexErrors++
						report.Warnings = append(report.Warnings, fmt.Sprintf(
							"source index: stale or inconsistent secondary for source %q", sid,
						))
						if errLimit > 0 && report.SourceIndexErrors >= errLimit {
							report.Warnings = append(report.Warnings,
								fmt.Sprintf("source index check stopped at MaxErrors=%d", errLimit))
							return false
						}
					}
					return true
				},
			); err != nil {
				return fmt.Errorf("hexxladb: health source index scan: %w", err)
			}
			if srcScanErr != nil {
				return srcScanErr
			}
		}

		return nil
	}); err != nil {
		return HealthReport{}, err
	}

	return report, nil
}

// secondaryTagIndexConsistent verifies a physical tag/ key against primary storage.
// Malformed keys are ignored (caller should not increment errors). MVCC suffixed keys
// are validated against cell/<packed><seq>; v1 keys and unsuffixed tag keys check live head cells only.
func secondaryTagIndexConsistent(tx *Tx, dbMVCC bool, k []byte, live map[lattice.PackedCoord]struct{}) (bool, error) {
	tag, p, seq, hasSeq, err := index.ParseTagKeyWithSeq(k)
	if err != nil {
		return true, nil // skip malformed keys
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
			return false, nil
		}
		return slices.Contains(rec.Tags, tag), nil
	}
	_, alive := live[p]
	return alive, nil
}

func secondarySourceIndexConsistent(tx *Tx, dbMVCC bool, k []byte, live map[lattice.PackedCoord]struct{}) (bool, error) {
	sid, p, seq, hasSeq, err := index.ParseSourceKeyWithSeq(k)
	if err != nil {
		return true, nil // skip malformed keys
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
			return false, nil
		}
		return rec.Provenance.SourceID == sid, nil
	}
	_, alive := live[p]
	return alive, nil
}
