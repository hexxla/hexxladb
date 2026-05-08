package hexxladb_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hexxla/hexxladb"
	"github.com/hexxla/hexxladb/internal/index"
	"github.com/hexxla/hexxladb/internal/lattice"
	"github.com/hexxla/hexxladb/internal/record"
)

// benchAPIPreloadCells inserts n distinct cells on a grid and returns open db + first packed key for reads.
func benchAPIPreloadCells(b *testing.B, n int) (*hexxladb.DB, lattice.PackedCoord) {
	b.Helper()
	return benchAPIPreloadCellsWithOptions(b, n, nil)
}

func benchAPIPreloadCellsWithOptions(b *testing.B, n int, opts *hexxladb.Options) (*hexxladb.DB, lattice.PackedCoord) {
	b.Helper()
	path := filepath.Join(b.TempDir(), "api_bench.db")
	db, err := hexxladb.Open(path, opts)
	if err != nil {
		b.Fatal(err)
	}
	vf := int64(2) * index.WeekNanos
	nq := 200
	var first lattice.PackedCoord
	err = db.Update(func(tx *hexxladb.Tx) error {
		for i := range n {
			q := i % nq
			r := i / nq
			p, err := lattice.Pack(lattice.Coord{Q: q, R: r})
			if err != nil {
				return err
			}
			if i == 0 {
				first = p
			}
			rec := record.CellRecord{
				Key:        p,
				RawContent: fmt.Sprintf("c%d", i),
				Provenance: record.ProvenanceWire{
					SourceID:   "bench-preload",
					Confidence: 1,
					CreatedAt:  int64(i),
					UpdatedAt:  int64(i),
				},
				Validity: record.ValidityWire{ValidFrom: &vf},
			}
			if err := tx.PutCell(context.Background(), rec); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		b.Fatal(err)
	}
	return db, first
}

// apiBenchPreloadSizes returns preload row counts for read/scan API benchmarks.
// Default is 512 and 2000 so plain `go test -bench=BenchmarkAPI` stays tolerable.
//
//   - HEXXLA_BENCH_PRELOAD=all — adds cells_10000 (used by make bench-stress).
//   - HEXXLA_BENCH_PRELOAD=extreme — also adds cells_50000 (many large DBs under $TMPDIR; can fill small /tmp).
//
// Set TMPDIR to a volume with plenty of free space when using all or extreme.
func apiBenchPreloadSizes(b *testing.B) []int {
	b.Helper()
	switch os.Getenv("HEXXLA_BENCH_PRELOAD") {
	case "extreme":
		return []int{512, 2000, 10000, 50000}
	case "all":
		return []int{512, 2000, 10000}
	default:
		return []int{512, 2000}
	}
}

// BenchmarkAPI_PutCell measures one [Tx.PutCell] per [DB.Update] with a fresh logical key each iteration.
func BenchmarkAPI_PutCell(b *testing.B) {
	path := filepath.Join(b.TempDir(), "putcell.db")
	db, err := hexxladb.Open(path, nil)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = db.Close() })
	vf := int64(3) * index.WeekNanos
	i := 0
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		q := i % 400
		r := i / 400
		i++
		p, err := lattice.Pack(lattice.Coord{Q: q, R: r})
		if err != nil {
			b.Fatal(err)
		}
		rec := record.CellRecord{
			Key:        p,
			RawContent: "bench",
			Provenance: record.ProvenanceWire{SourceID: "bench", Confidence: 1, CreatedAt: 1, UpdatedAt: 1},
			Validity:   record.ValidityWire{ValidFrom: &vf},
		}
		err = db.Update(func(tx *hexxladb.Tx) error {
			return tx.PutCell(context.Background(), rec)
		})
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkAPI_PutCell_MVCC compares per-write overhead with MVCC enabled.
func BenchmarkAPI_PutCell_MVCC(b *testing.B) {
	path := filepath.Join(b.TempDir(), "putcell_mvcc.db")
	db, err := hexxladb.Open(path, &hexxladb.Options{EnableMVCC: true})
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = db.Close() })
	vf := int64(3) * index.WeekNanos
	i := 0
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		q := i % 400
		r := i / 400
		i++
		p, err := lattice.Pack(lattice.Coord{Q: q, R: r})
		if err != nil {
			b.Fatal(err)
		}
		rec := record.CellRecord{
			Key:        p,
			RawContent: "bench",
			Provenance: record.ProvenanceWire{SourceID: "bench", Confidence: 1, CreatedAt: 1, UpdatedAt: 1},
			Validity:   record.ValidityWire{ValidFrom: &vf},
		}
		if err := db.Update(func(tx *hexxladb.Tx) error { return tx.PutCell(context.Background(), rec) }); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkAPI_GetCell measures random-access read after preloading n cells (sub-benchmark per n).
func BenchmarkAPI_GetCell(b *testing.B) {
	for _, n := range apiBenchPreloadSizes(b) {
		b.Run(fmt.Sprintf("cells_%d", n), func(b *testing.B) {
			db, key := benchAPIPreloadCells(b, n)
			b.Cleanup(func() { _ = db.Close() })
			b.ReportMetric(float64(n), "cells")
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				err := db.View(func(tx *hexxladb.Tx) error {
					_, _, err := tx.GetCell(key)
					return err
				})
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkAPI_GetCell_Encrypted(b *testing.B) {
	for _, n := range apiBenchPreloadSizes(b) {
		b.Run(fmt.Sprintf("cells_%d", n), func(b *testing.B) {
			opts := &hexxladb.Options{EncryptionKey: []byte("sixteen.byte.key!!")}
			db, key := benchAPIPreloadCellsWithOptions(b, n, opts)
			b.Cleanup(func() { _ = db.Close() })
			b.ReportMetric(float64(n), "cells")
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				err := db.View(func(tx *hexxladb.Tx) error {
					_, _, err := tx.GetCell(key)
					return err
				})
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkAPI_GetCell_MVCC_Encrypted composes MVCC + at-rest encryption on one open Options.
func BenchmarkAPI_GetCell_MVCC_Encrypted(b *testing.B) {
	for _, n := range apiBenchPreloadSizes(b) {
		b.Run(fmt.Sprintf("cells_%d", n), func(b *testing.B) {
			opts := &hexxladb.Options{
				EnableMVCC:    true,
				EncryptionKey: []byte("sixteen.byte.key!!"),
			}
			db, key := benchAPIPreloadCellsWithOptions(b, n, opts)
			b.Cleanup(func() { _ = db.Close() })
			b.ReportMetric(float64(n), "cells")
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				err := db.View(func(tx *hexxladb.Tx) error {
					_, _, err := tx.GetCell(key)
					return err
				})
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkAPI_ViewUpdateContention measures mixed readers/writers on one DB.
func BenchmarkAPI_ViewUpdateContention(b *testing.B) {
	db, key := benchAPIPreloadCells(b, 2000)
	b.Cleanup(func() { _ = db.Close() })
	var ops atomic.Uint64
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			n := ops.Add(1)
			if n%20 == 0 {
				_ = db.Update(func(tx *hexxladb.Tx) error {
					return tx.Put([]byte("contended"), []byte("v"))
				})
				continue
			}
			_ = db.View(func(tx *hexxladb.Tx) error {
				_, _, err := tx.GetCell(key)
				return err
			})
		}
	})
}

// BenchmarkAPI_AscendCellsBySource scans the source/ index after preload (sub-benchmark per n).
func BenchmarkAPI_AscendCellsBySource(b *testing.B) {
	for _, n := range apiBenchPreloadSizes(b) {
		b.Run(fmt.Sprintf("cells_%d", n), func(b *testing.B) {
			db, _ := benchAPIPreloadCells(b, n)
			b.Cleanup(func() { _ = db.Close() })
			b.ReportMetric(float64(n), "cells")
			ctx := context.Background()
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				var count int
				err := db.View(func(tx *hexxladb.Tx) error {
					return tx.AscendCellsBySource(ctx, "bench-preload", func(record.CellRecord) bool {
						count++
						return true
					})
				})
				if err != nil {
					b.Fatal(err)
				}
				if count != n {
					b.Fatalf("count=%d want %d", count, n)
				}
			}
		})
	}
}

// BenchmarkAPI_LoadContext reads a small neighborhood after preload (sub-benchmark per n).
func BenchmarkAPI_LoadContext(b *testing.B) {
	for _, n := range apiBenchPreloadSizes(b) {
		b.Run(fmt.Sprintf("cells_%d", n), func(b *testing.B) {
			db, _ := benchAPIPreloadCells(b, n)
			b.Cleanup(func() { _ = db.Close() })
			b.ReportMetric(float64(n), "cells")
			center := lattice.Coord{Q: 0, R: 0}
			ctx := context.Background()
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				var cells []record.CellRecord
				err := db.View(func(tx *hexxladb.Tx) error {
					var inner error
					cells, inner = tx.ScanContextRaw(ctx, center, 3, 50)
					return inner
				})
				if err != nil {
					b.Fatal(err)
				}
				_ = cells
			}
		})
	}
}

// walkBenchRing is fixed ring index for WalkRing / WalkRingAt benches (ring k has 6k cells for k>=1).
const walkBenchRing = 2

func benchValidAsOf() time.Time {
	vf := int64(2) * index.WeekNanos // matches preload validity in benchAPIPreloadCells
	t := vf + 1
	return time.Unix(t/1e9, t%1e9).UTC()
}

// BenchmarkAPI_LoadContextAt is like BenchmarkAPI_LoadContext but applies [record.ValidAt] at asOf.
func BenchmarkAPI_LoadContextAt(b *testing.B) {
	asOf := benchValidAsOf()
	center := lattice.Coord{Q: 0, R: 0}
	ctx := context.Background()
	for _, n := range apiBenchPreloadSizes(b) {
		b.Run(fmt.Sprintf("cells_%d", n), func(b *testing.B) {
			db, _ := benchAPIPreloadCells(b, n)
			b.Cleanup(func() { _ = db.Close() })
			b.ReportMetric(float64(n), "cells")
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				var cells []record.CellRecord
				err := db.View(func(tx *hexxladb.Tx) error {
					var inner error
					cells, inner = tx.ScanContextAtRaw(ctx, center, 3, 50, asOf)
					return inner
				})
				if err != nil {
					b.Fatal(err)
				}
				_ = cells
			}
		})
	}
}

// BenchmarkAPI_WalkRing visits one hex ring ([Tx.WalkRing]) after DB preload.
func BenchmarkAPI_WalkRing(b *testing.B) {
	center := lattice.Coord{Q: 0, R: 0}
	ctx := context.Background()
	ringN := len(lattice.Ring(center, walkBenchRing))
	for _, n := range apiBenchPreloadSizes(b) {
		b.Run(fmt.Sprintf("cells_%d", n), func(b *testing.B) {
			db, _ := benchAPIPreloadCells(b, n)
			b.Cleanup(func() { _ = db.Close() })
			b.ReportMetric(float64(n), "cells")
			b.ReportMetric(float64(ringN), "ring_cells")
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				err := db.View(func(tx *hexxladb.Tx) error {
					return tx.WalkRing(ctx, center, walkBenchRing, func(lattice.Coord, []byte, bool) bool {
						return true
					})
				})
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkAPI_QueryCells measures [Tx.QueryCells] with varying predicate complexity.
// Sub-benchmarks: tag-only, source-only, spatial, and combined predicates.
func BenchmarkAPI_QueryCells(b *testing.B) {
	for _, n := range apiBenchPreloadSizes(b) {
		db, _ := benchAPIPreloadCells(b, n)
		b.Cleanup(func() { _ = db.Close() })
		ctx := context.Background()
		center := lattice.Coord{Q: 0, R: 0}

		b.Run(fmt.Sprintf("tag_only/cells_%d", n), func(b *testing.B) {
			b.ReportMetric(float64(n), "cells")
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				err := db.View(func(tx *hexxladb.Tx) error {
					_, err := tx.QueryCells(ctx, hexxladb.CellQuery{
						RequireTags: []string{"nonexistent"},
						MaxResults:  50,
					})
					return err
				})
				if err != nil {
					b.Fatal(err)
				}
			}
		})

		b.Run(fmt.Sprintf("source_only/cells_%d", n), func(b *testing.B) {
			b.ReportMetric(float64(n), "cells")
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				err := db.View(func(tx *hexxladb.Tx) error {
					_, err := tx.QueryCells(ctx, hexxladb.CellQuery{
						SourceID:   "bench-preload",
						MaxResults: 50,
					})
					return err
				})
				if err != nil {
					b.Fatal(err)
				}
			}
		})

		b.Run(fmt.Sprintf("spatial/cells_%d", n), func(b *testing.B) {
			b.ReportMetric(float64(n), "cells")
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				err := db.View(func(tx *hexxladb.Tx) error {
					_, err := tx.QueryCells(ctx, hexxladb.CellQuery{
						Center:     center,
						Radius:     3,
						MaxResults: 50,
					})
					return err
				})
				if err != nil {
					b.Fatal(err)
				}
			}
		})

		b.Run(fmt.Sprintf("combined/cells_%d", n), func(b *testing.B) {
			b.ReportMetric(float64(n), "cells")
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				err := db.View(func(tx *hexxladb.Tx) error {
					_, err := tx.QueryCells(ctx, hexxladb.CellQuery{
						SourceID:      "bench-preload",
						Center:        center,
						Radius:        5,
						MinConfidence: 0.5,
						MaxResults:    50,
						SortBy:        hexxladb.SortByConfidence,
					})
					return err
				})
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkAPI_LoadContext_Budgeted measures [Tx.LoadContext] with budgeted assembly at varying radii.
func BenchmarkAPI_LoadContext_Budgeted(b *testing.B) {
	for _, n := range apiBenchPreloadSizes(b) {
		db, _ := benchAPIPreloadCells(b, n)
		b.Cleanup(func() { _ = db.Close() })
		ctx := context.Background()
		center := lattice.Coord{Q: 0, R: 0}
		assembly := hexxladb.LoadContextBudgetConfig{
			Assemble: hexxladb.DefaultAssembleCellViewOpts(),
		}

		for _, radius := range []int{1, 3, 5} {
			b.Run(fmt.Sprintf("r%d/cells_%d", radius, n), func(b *testing.B) {
				b.ReportMetric(float64(n), "cells")
				b.ReportMetric(float64(radius), "radius")
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					err := db.View(func(tx *hexxladb.Tx) error {
						_, err := tx.LoadContext(ctx, hexxladb.LoadContextConfig{
							Seeds:     []hexxladb.Coord{center},
							MaxRing:   radius,
							MaxTokens: 4096,
							Assembly:  assembly,
						})
						return err
					})
					if err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

// BenchmarkAPI_MVCCVersionResolution measures MVCC [Tx.GetCell] with high version counts.
// Each sub-benchmark writes the same coord N times then benchmarks the GetCell read path,
// which drives [internal/mvcc.SelectVisible] — the O(n) version scan.
func BenchmarkAPI_MVCCVersionResolution(b *testing.B) {
	for _, versions := range []int{10, 50, 100, 500} {
		b.Run(fmt.Sprintf("versions_%d", versions), func(b *testing.B) {
			path := filepath.Join(b.TempDir(), "mvcc_versions.db")
			db, err := hexxladb.Open(path, &hexxladb.Options{EnableMVCC: true})
			if err != nil {
				b.Fatal(err)
			}
			b.Cleanup(func() { _ = db.Close() })

			p, err := lattice.Pack(lattice.Coord{Q: 0, R: 0})
			if err != nil {
				b.Fatal(err)
			}
			vf := int64(2) * index.WeekNanos
			for i := range versions {
				err := db.Update(func(tx *hexxladb.Tx) error {
					return tx.PutCell(context.Background(), record.CellRecord{
						Key:        p,
						RawContent: fmt.Sprintf("version-%d", i),
						Provenance: record.ProvenanceWire{
							SourceID:   "bench",
							Confidence: 1,
							CreatedAt:  int64(i),
							UpdatedAt:  int64(i),
						},
						Validity: record.ValidityWire{ValidFrom: &vf},
					})
				})
				if err != nil {
					b.Fatal(err)
				}
			}

			b.ReportMetric(float64(versions), "versions")
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				err := db.View(func(tx *hexxladb.Tx) error {
					_, _, err := tx.GetCell(p)
					return err
				})
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkAPI_FindSeams measures [Tx.FindSeams] latency with varying numbers of seams stored near center.
// Sub-benchmarks: 0, 10, 50, 100 seams within radius 3 of origin.
func BenchmarkAPI_FindSeams(b *testing.B) {
	ctx := context.Background()
	center := lattice.Coord{Q: 0, R: 0}

	for _, nSeams := range []int{0, 10, 50, 100} {
		b.Run(fmt.Sprintf("seams_%d", nSeams), func(b *testing.B) {
			path := filepath.Join(b.TempDir(), "findseams.db")
			db, err := hexxladb.Open(path, nil)
			if err != nil {
				b.Fatal(err)
			}
			b.Cleanup(func() { _ = db.Close() })

			vf := int64(2) * index.WeekNanos
			err = db.Update(func(tx *hexxladb.Tx) error {
				for i := range nSeams {
					qa, ra := i%10, i/10
					qb, rb := (i+1)%10, (i+1)/10
					pa, err := lattice.Pack(lattice.Coord{Q: qa, R: ra})
					if err != nil {
						return err
					}
					pb, err := lattice.Pack(lattice.Coord{Q: qb, R: rb})
					if err != nil {
						return err
					}
					cellA := record.CellRecord{Key: pa, RawContent: fmt.Sprintf("a%d", i), Provenance: record.ProvenanceWire{SourceID: "bench", Confidence: 1, CreatedAt: int64(i), UpdatedAt: int64(i)}, Validity: record.ValidityWire{ValidFrom: &vf}}
					cellB := record.CellRecord{Key: pb, RawContent: fmt.Sprintf("b%d", i), Provenance: record.ProvenanceWire{SourceID: "bench", Confidence: 1, CreatedAt: int64(i), UpdatedAt: int64(i)}, Validity: record.ValidityWire{ValidFrom: &vf}}
					if err := tx.PutCell(ctx, cellA); err != nil {
						return err
					}
					if err := tx.PutCell(ctx, cellB); err != nil {
						return err
					}
					if err := tx.MarkConflict(hexxladb.Coord{Q: qa, R: ra}, hexxladb.Coord{Q: qb, R: rb}, fmt.Sprintf("conflict-%d", i)); err != nil {
						return err
					}
				}
				return nil
			})
			if err != nil {
				b.Fatal(err)
			}

			b.ReportMetric(float64(nSeams), "seams")
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				err := db.View(func(tx *hexxladb.Tx) error {
					_, err := tx.FindSeams(ctx, center, 3, false)
					return err
				})
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkAPI_HealthCheck measures [DB.HealthCheck] with all checks enabled.
// The cell/ and seam/ primary-key forward scans mean cost is O(cells+seams),
// not O(ScanRadius²) as in the previous WalkRings implementation.
func BenchmarkAPI_HealthCheck(b *testing.B) {
	ctx := context.Background()
	cfg := hexxladb.DefaultHealthCheckConfig()
	for _, n := range apiBenchPreloadSizes(b) {
		b.Run(fmt.Sprintf("cells_%d", n), func(b *testing.B) {
			db, _ := benchAPIPreloadCells(b, n)
			b.Cleanup(func() { _ = db.Close() })
			b.ReportMetric(float64(n), "cells")
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if _, err := db.HealthCheck(ctx, cfg); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkAPI_BatchPutCells measures batch write throughput via [DB.BatchPutCells].
// Sub-benchmarks vary batch size (10/100/500); each iteration commits one full batch.
// cells/op shows how many cells are committed per iteration so throughput is readable directly.
func BenchmarkAPI_BatchPutCells(b *testing.B) {
	ctx := context.Background()
	nq := 200
	for _, batchSize := range []int{10, 100, 500} {
		b.Run(fmt.Sprintf("batch_%d", batchSize), func(b *testing.B) {
			cells := make([]record.CellRecord, batchSize)
			for i := range batchSize {
				q := i % nq
				r := i / nq
				p, err := lattice.Pack(lattice.Coord{Q: q, R: r})
				if err != nil {
					b.Fatal(err)
				}
				cells[i] = record.CellRecord{
					Key:        p,
					RawContent: fmt.Sprintf("batch-cell-%d", i),
					Provenance: record.ProvenanceWire{
						SourceID:   "bench-batch",
						Confidence: 0.9,
						CreatedAt:  int64(i),
						UpdatedAt:  int64(i),
					},
				}
			}
			opts := &hexxladb.BatchPutCellOptions{BatchSize: batchSize}
			b.ReportMetric(float64(batchSize), "cells/op")
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				path := filepath.Join(b.TempDir(), "batch_bench.db")
				db, err := hexxladb.Open(path, nil)
				if err != nil {
					b.Fatal(err)
				}
				if _, err := db.BatchPutCells(ctx, cells, opts); err != nil {
					_ = db.Close()
					b.Fatal(err)
				}
				_ = db.Close()
				_ = os.RemoveAll(path)
			}
		})
	}
}

// BenchmarkAPI_WalkRingAt visits one ring with validity filtering ([Tx.WalkRingAt]).
func BenchmarkAPI_WalkRingAt(b *testing.B) {
	asOf := benchValidAsOf()
	center := lattice.Coord{Q: 0, R: 0}
	ctx := context.Background()
	ringN := len(lattice.Ring(center, walkBenchRing))
	for _, n := range apiBenchPreloadSizes(b) {
		b.Run(fmt.Sprintf("cells_%d", n), func(b *testing.B) {
			db, _ := benchAPIPreloadCells(b, n)
			b.Cleanup(func() { _ = db.Close() })
			b.ReportMetric(float64(n), "cells")
			b.ReportMetric(float64(ringN), "ring_cells")
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				err := db.View(func(tx *hexxladb.Tx) error {
					return tx.WalkRingAt(ctx, center, walkBenchRing, asOf, func(lattice.Coord, record.CellRecord) bool {
						return true
					})
				})
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
