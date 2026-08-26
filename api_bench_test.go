package hexxladb_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
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
//   - HEXXLA_BENCH_PRELOAD=all — adds cells_10000 (used by task bench-stress).
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

// BenchmarkAPI_WritePath measures the public write path with the durability and
// batching controls that materially affect user-visible latency.
func BenchmarkAPI_WritePath(b *testing.B) {
	type writeCase struct {
		name          string
		opts          hexxladb.Options
		cellsPerWrite int
		callbackDelay time.Duration
	}
	cases := []writeCase{
		{name: "single_default", opts: hexxladb.Options{EnableMVCC: true}, cellsPerWrite: 1},
		{name: "single_wait_10ms", opts: hexxladb.Options{EnableMVCC: true, GroupWALMaxBatchWait: 10 * time.Millisecond}, cellsPerWrite: 1},
		{name: "single_fdatasync", opts: hexxladb.Options{EnableMVCC: true, UsePrimaryFdatasync: true}, cellsPerWrite: 1},
		{name: "single_callback_1ms", opts: hexxladb.Options{EnableMVCC: true}, cellsPerWrite: 1, callbackDelay: time.Millisecond},
		{name: "batch_100", opts: hexxladb.Options{EnableMVCC: true}, cellsPerWrite: 100},
	}
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			db, err := hexxladb.Open(filepath.Join(b.TempDir(), "write_path.db"), &tc.opts)
			if err != nil {
				b.Fatal(err)
			}
			b.Cleanup(func() { _ = db.Close() })
			next := 0
			b.ReportMetric(float64(tc.cellsPerWrite), "cells/op")
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				err := db.Update(func(tx *hexxladb.Tx) error {
					if tc.callbackDelay > 0 {
						time.Sleep(tc.callbackDelay)
					}
					for i := range tc.cellsPerWrite {
						coord := lattice.Coord{Q: (next + i) % 100_000, R: (next + i) / 100_000}
						p, err := lattice.Pack(coord)
						if err != nil {
							return err
						}
						if err := tx.PutCell(context.Background(), record.CellRecord{Key: p, RawContent: "write-path"}); err != nil {
							return err
						}
					}
					next += tc.cellsPerWrite
					return nil
				})
				if err != nil {
					b.Fatal(err)
				}
			}
			b.StopTimer()
			reportWriteMetrics(b, db)
			reportGroupWALMetrics(b, db)
		})
	}
}

// BenchmarkAPI_WriteReaderBlocking measures how long a View waits behind an
// Update whose callback deliberately remains active for one millisecond.
func BenchmarkAPI_WriteReaderBlocking(b *testing.B) {
	db, err := hexxladb.Open(filepath.Join(b.TempDir(), "reader_blocking.db"), &hexxladb.Options{EnableMVCC: true})
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = db.Close() })
	next := 0
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		started := make(chan struct{})
		done := make(chan error, 1)
		coord := lattice.Coord{Q: next % 100_000, R: next / 100_000}
		next++
		p, err := lattice.Pack(coord)
		if err != nil {
			b.Fatal(err)
		}
		var wg sync.WaitGroup
		wg.Go(func() {
			done <- db.Update(func(tx *hexxladb.Tx) error {
				close(started)
				time.Sleep(time.Millisecond)
				return tx.PutCell(context.Background(), record.CellRecord{Key: p, RawContent: "reader-blocking"})
			})
		})
		<-started
		if err := db.View(func(*hexxladb.Tx) error { return nil }); err != nil {
			b.Fatal(err)
		}
		wg.Wait()
		if err := <-done; err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	reportGroupWALMetrics(b, db)
}

func reportGroupWALMetrics(b *testing.B, db *hexxladb.DB) {
	b.Helper()
	applyBatches, multiJobBatches, walSyncs := db.GroupWALStats()
	operations := float64(max(b.N, 1))
	b.ReportMetric(float64(applyBatches)/operations, "apply-batches/op")
	b.ReportMetric(float64(multiJobBatches)/operations, "multi-job-batches/op")
	b.ReportMetric(float64(walSyncs)/operations, "wal-syncs/op")
}

func reportWriteMetrics(b *testing.B, db *hexxladb.DB) {
	b.Helper()
	stats := db.WriteStats()
	calls := float64(max(stats.Calls, 1))
	b.ReportMetric(float64(stats.LockWait.Nanoseconds())/calls, "lock-wait-ns/call")
	b.ReportMetric(float64(stats.Callback.Nanoseconds())/calls, "callback-ns/call")
	b.ReportMetric(float64(stats.Durability.Nanoseconds())/calls, "durability-ns/call")
	b.ReportMetric(float64(stats.Finalization.Nanoseconds())/calls, "finalization-ns/call")
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

func BenchmarkAPI_GetCell_MVCC(b *testing.B) {
	for _, n := range apiBenchPreloadSizes(b) {
		b.Run(fmt.Sprintf("cells_%d", n), func(b *testing.B) {
			db, key := benchAPIPreloadCellsWithOptions(b, n, &hexxladb.Options{EnableMVCC: true})
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
	var firstErr error
	var errOnce sync.Once
	recordErr := func(err error) {
		if err != nil {
			errOnce.Do(func() { firstErr = err })
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			n := ops.Add(1)
			if n%20 == 0 {
				recordErr(db.Update(func(tx *hexxladb.Tx) error {
					return tx.Put([]byte("contended"), []byte("v"))
				}))
				continue
			}
			recordErr(db.View(func(tx *hexxladb.Tx) error {
				_, _, err := tx.GetCell(key)
				return err
			}))
		}
	})
	if firstErr != nil {
		b.Fatal(firstErr)
	}
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

// BenchmarkAPI_ScanContextRaw reads a small raw-record neighborhood after preload.
func BenchmarkAPI_ScanContextRaw(b *testing.B) {
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

// BenchmarkAPI_ScanContextAtRaw is like BenchmarkAPI_ScanContextRaw but applies [record.ValidAt] at asOf.
func BenchmarkAPI_ScanContextAtRaw(b *testing.B) {
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

// BenchmarkAPI_LoadContext measures [Tx.LoadContext] with result-bounded assembly at varying radii.
func BenchmarkAPI_LoadContext(b *testing.B) {
	for _, n := range apiBenchPreloadSizes(b) {
		db, _ := benchAPIPreloadCells(b, n)
		b.Cleanup(func() { _ = db.Close() })
		ctx := context.Background()
		center := lattice.Coord{Q: 0, R: 0}
		assembly := hexxladb.ContextAssemblyConfig{
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
							Seeds:    []hexxladb.Coord{center},
							MaxRing:  radius,
							MaxCells: 64,
							Assembly: assembly,
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

// BenchmarkAPI_MVCCVersionResolution measures latest and historical MVCC [Tx.GetCell]
// lookups with high version counts on one logical coordinate.
func BenchmarkAPI_MVCCVersionResolution(b *testing.B) {
	for _, versions := range []int{10, 100, 1000, 6000} {
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

			for _, tc := range []struct {
				name    string
				readSeq uint64
				want    string
			}{
				{name: "latest", readSeq: uint64(versions), want: fmt.Sprintf("version-%d", versions-1)},
				{name: "historical", readSeq: uint64(versions / 2), want: fmt.Sprintf("version-%d", versions/2-1)},
			} {
				b.Run(tc.name, func(b *testing.B) {
					if err := db.ViewAt(tc.readSeq, func(tx *hexxladb.Tx) error {
						rec, ok, err := tx.GetCell(p)
						if err != nil {
							return err
						}
						if !ok || rec.RawContent != tc.want {
							return fmt.Errorf("read_seq %d: ok=%v content=%q want=%q", tc.readSeq, ok, rec.RawContent, tc.want)
						}
						return nil
					}); err != nil {
						b.Fatal(err)
					}
					b.ReportMetric(float64(versions), "versions")
					b.ReportAllocs()
					b.ResetTimer()
					for b.Loop() {
						err := db.ViewAt(tc.readSeq, func(tx *hexxladb.Tx) error {
							_, _, err := tx.GetCell(p)
							return err
						})
						if err != nil {
							b.Fatal(err)
						}
					}
				})
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
			db, err := hexxladb.Open(filepath.Join(b.TempDir(), "batch_bench.db"), &hexxladb.Options{EnableMVCC: true})
			if err != nil {
				b.Fatal(err)
			}
			b.Cleanup(func() { _ = db.Close() })
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
			next := 0
			b.ReportMetric(float64(batchSize), "cells/op")
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				for i := range batchSize {
					coord := lattice.Coord{Q: (next + i) % 100_000, R: (next + i) / 100_000}
					p, err := lattice.Pack(coord)
					if err != nil {
						b.Fatal(err)
					}
					cells[i].Key = p
				}
				next += batchSize
				if _, err := db.BatchPutCells(ctx, cells, opts); err != nil {
					b.Fatal(err)
				}
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

// BenchmarkAPI_SearchCells measures [Tx.SearchCells] lexical search with varying query complexity.
// Sub-benchmarks: single-term hit, multi-term (two tokens), tag-filter-only.
func BenchmarkAPI_SearchCells(b *testing.B) {
	for _, n := range apiBenchPreloadSizes(b) {
		db, _ := benchAPIPreloadCells(b, n)
		b.Cleanup(func() { _ = db.Close() })
		ctx := context.Background()

		b.Run(fmt.Sprintf("single_term/cells_%d", n), func(b *testing.B) {
			b.ReportMetric(float64(n), "cells")
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				err := db.View(func(tx *hexxladb.Tx) error {
					_, err := tx.SearchCells(ctx, hexxladb.CellSearchConfig{
						Query:      "bench",
						MaxResults: 20,
					})
					return err
				})
				if err != nil {
					b.Fatal(err)
				}
			}
		})

		b.Run(fmt.Sprintf("multi_term/cells_%d", n), func(b *testing.B) {
			b.ReportMetric(float64(n), "cells")
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				err := db.View(func(tx *hexxladb.Tx) error {
					_, err := tx.SearchCells(ctx, hexxladb.CellSearchConfig{
						Query:      "bench preload",
						MaxResults: 20,
					})
					return err
				})
				if err != nil {
					b.Fatal(err)
				}
			}
		})

		b.Run(fmt.Sprintf("no_match/cells_%d", n), func(b *testing.B) {
			b.ReportMetric(float64(n), "cells")
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				err := db.View(func(tx *hexxladb.Tx) error {
					_, err := tx.SearchCells(ctx, hexxladb.CellSearchConfig{
						Query:      "zzz_nonexistent_xyzzy",
						MaxResults: 20,
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

// BenchmarkAPI_LoadContextFOV measures [Tx.LoadContextFOV] — visibility-filtered ring walk.
// opaque returns false for all cells (open field) to measure pure scan overhead.
func BenchmarkAPI_LoadContextFOV(b *testing.B) {
	center := lattice.Coord{Q: 0, R: 0}
	ctx := context.Background()
	opaque := func(hexxladb.Coord) bool { return false }
	for _, n := range apiBenchPreloadSizes(b) {
		for _, radius := range []int{3, 5} {
			b.Run(fmt.Sprintf("r%d/cells_%d", radius, n), func(b *testing.B) {
				db, _ := benchAPIPreloadCells(b, n)
				b.Cleanup(func() { _ = db.Close() })
				b.ReportMetric(float64(n), "cells")
				b.ReportMetric(float64(radius), "radius")
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					err := db.View(func(tx *hexxladb.Tx) error {
						_, err := tx.LoadContextFOV(ctx, center, radius, opaque, hexxladb.FOVContextConfig{MaxCells: 200})
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

// BenchmarkAPI_LoadContextVoronoi measures [Tx.LoadContextVoronoi] with 2 and 4 seeds.
func BenchmarkAPI_LoadContextVoronoi(b *testing.B) {
	seeds2 := []hexxladb.Coord{{Q: 0, R: 0}, {Q: 10, R: 0}}
	seeds4 := []hexxladb.Coord{{Q: 0, R: 0}, {Q: 10, R: 0}, {Q: 0, R: 10}, {Q: 10, R: 10}}
	ctx := context.Background()
	cfg := hexxladb.VoronoiContextConfig{MaxRadius: 4, MaxCellsPerSeed: 50}

	for _, n := range apiBenchPreloadSizes(b) {
		db, _ := benchAPIPreloadCells(b, n)
		b.Cleanup(func() { _ = db.Close() })

		b.Run(fmt.Sprintf("seeds_2/cells_%d", n), func(b *testing.B) {
			b.ReportMetric(float64(n), "cells")
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				err := db.View(func(tx *hexxladb.Tx) error {
					_, err := tx.LoadContextVoronoi(ctx, seeds2, cfg)
					return err
				})
				if err != nil {
					b.Fatal(err)
				}
			}
		})

		b.Run(fmt.Sprintf("seeds_4/cells_%d", n), func(b *testing.B) {
			b.ReportMetric(float64(n), "cells")
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				err := db.View(func(tx *hexxladb.Tx) error {
					_, err := tx.LoadContextVoronoi(ctx, seeds4, cfg)
					return err
				})
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkAPI_PutEmbedding measures [Tx.PutEmbedding] write cost including HNSW graph maintenance.
// Sub-benchmarks vary vector dimension to capture the encode+graph-insert cost curve.
func BenchmarkAPI_PutEmbedding(b *testing.B) {
	ctx := context.Background()
	for _, dim := range []int{32, 128, 384} {
		b.Run(fmt.Sprintf("dim_%d", dim), func(b *testing.B) {
			path := filepath.Join(b.TempDir(), fmt.Sprintf("embed_%d.db", dim))
			db, err := hexxladb.Open(path, &hexxladb.Options{
				EmbeddingDimension: uint16(dim),
				DistanceMetric:     hexxladb.DistanceCosine,
				PageSize:           4096,
				MaxValueBytes:      65536,
			})
			if err != nil {
				b.Fatal(err)
			}
			b.Cleanup(func() { _ = db.Close() })

			vec := make([]float32, dim)
			for i := range vec {
				vec[i] = float32(i+1) / float32(dim)
			}
			i := 0
			b.ReportMetric(float64(dim), "dim")
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				q := i / 1000
				r := i % 1000
				i++
				p, pErr := lattice.Pack(lattice.Coord{Q: q, R: r})
				if pErr != nil {
					b.Fatal(pErr)
				}
				err := db.Update(func(tx *hexxladb.Tx) error {
					if err := tx.PutCell(ctx, record.CellRecord{
						Key:        p,
						RawContent: "bench",
						Provenance: record.ProvenanceWire{SourceID: "bench", Confidence: 1},
					}); err != nil {
						return err
					}
					return tx.PutEmbedding(p, vec)
				})
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkAPI_QueryCells_Embedding measures [Tx.QueryCells] with an embedding vector,
// exercising the full HNSW ANN + post-filter pipeline.
func BenchmarkAPI_QueryCells_Embedding(b *testing.B) {
	ctx := context.Background()
	for _, tc := range []struct {
		n   int
		dim int
	}{
		{500, 32},
		{500, 128},
	} {
		b.Run(fmt.Sprintf("n%d_dim%d", tc.n, tc.dim), func(b *testing.B) {
			path := filepath.Join(b.TempDir(), "qce.db")
			db, err := hexxladb.Open(path, &hexxladb.Options{
				EmbeddingDimension: uint16(tc.dim),
				DistanceMetric:     hexxladb.DistanceCosine,
				PageSize:           4096,
				MaxValueBytes:      65536,
			})
			if err != nil {
				b.Fatal(err)
			}
			b.Cleanup(func() { _ = db.Close() })

			vec := make([]float32, tc.dim)
			for i := range vec {
				vec[i] = float32(i+1) / float32(tc.dim)
			}
			for i := range tc.n {
				q := i / 1000
				r := i % 1000
				p, err := lattice.Pack(lattice.Coord{Q: q, R: r})
				if err != nil {
					b.Fatal(err)
				}
				if err := db.Update(func(tx *hexxladb.Tx) error {
					if err := tx.PutCell(ctx, record.CellRecord{Key: p, RawContent: "bench", Provenance: record.ProvenanceWire{SourceID: "bench", Confidence: 1}}); err != nil {
						return err
					}
					return tx.PutEmbedding(p, vec)
				}); err != nil {
					b.Fatal(err)
				}
			}

			b.ReportMetric(float64(tc.n), "cells")
			b.ReportMetric(float64(tc.dim), "dim")
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				err := db.View(func(tx *hexxladb.Tx) error {
					_, err := tx.QueryCells(ctx, hexxladb.CellQuery{
						Embedding:  vec,
						MaxResults: 10,
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

// BenchmarkAPI_SnapshotDiff measures [DB.SnapshotDiff] over a range of commit sequences.
// Sub-benchmarks vary the number of cells written in the diff window (10, 100, 500).
func BenchmarkAPI_SnapshotDiff(b *testing.B) {
	for _, nWrites := range []int{10, 100, 500} {
		b.Run(fmt.Sprintf("writes_%d", nWrites), func(b *testing.B) {
			path := filepath.Join(b.TempDir(), "snapdiff.db")
			db, err := hexxladb.Open(path, &hexxladb.Options{EnableMVCC: true})
			if err != nil {
				b.Fatal(err)
			}
			b.Cleanup(func() { _ = db.Close() })
			ctx := context.Background()
			vf := int64(2) * index.WeekNanos

			s0, err := db.StatsMVCC()
			if err != nil {
				b.Fatal(err)
			}
			fromSeq := s0.CommitSeq
			for i := range nWrites {
				p, err := lattice.Pack(lattice.Coord{Q: i % 200, R: i / 200})
				if err != nil {
					b.Fatal(err)
				}
				if err := db.Update(func(tx *hexxladb.Tx) error {
					return tx.PutCell(ctx, record.CellRecord{
						Key:        p,
						RawContent: fmt.Sprintf("diff-%d", i),
						Provenance: record.ProvenanceWire{SourceID: "bench", Confidence: 1, CreatedAt: int64(i), UpdatedAt: int64(i)},
						Validity:   record.ValidityWire{ValidFrom: &vf},
					})
				}); err != nil {
					b.Fatal(err)
				}
			}
			s1, err := db.StatsMVCC()
			if err != nil {
				b.Fatal(err)
			}
			toSeq := s1.CommitSeq

			b.ReportMetric(float64(nWrites), "writes")
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				_, err := db.SnapshotDiff(ctx, fromSeq, toSeq, hexxladb.SnapshotDiffConfig{})
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkAPI_Compact measures [DB.Compact] copy-compaction cost at varying DB sizes.
func BenchmarkAPI_Compact(b *testing.B) {
	for _, n := range []int{512, 2000} {
		b.Run(fmt.Sprintf("cells_%d", n), func(b *testing.B) {
			db, _ := benchAPIPreloadCells(b, n)
			b.Cleanup(func() { _ = db.Close() })
			ctx := context.Background()
			b.ReportMetric(float64(n), "cells")
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				dest := filepath.Join(b.TempDir(), "compact_out.db")
				if err := db.Compact(ctx, dest); err != nil {
					b.Fatal(err)
				}
				_ = os.Remove(dest)
				_ = os.Remove(dest + "-wal")
			}
		})
	}
}

// BenchmarkAPI_SpatialLookupOrder compares the current nearest-first ring access
// order with Morton-sorted access to the same coordinate set. Dense and sparse
// stores expose the hit/miss trade-off; disabling the page cache makes repeated
// B+ tree traversal costs visible without changing query semantics.
func BenchmarkAPI_SpatialLookupOrder(b *testing.B) {
	center := lattice.Coord{}
	for _, cache := range []struct {
		name string
		size int64
	}{{"cache_default", 0}, {"cache_disabled", -1}} {
		for _, stride := range []int{1, 4} {
			densityName := "dense"
			if stride > 1 {
				densityName = "sparse_25pct"
			}
			db := benchPreloadSpatial(b, center, 32, stride, &hexxladb.Options{PageCacheSize: cache.size})
			b.Cleanup(func() { _ = db.Close() })

			for _, radius := range []int{10, 32} {
				ringKeys := lattice.WalkRingsPacked(center, radius)
				mortonKeys := slices.Clone(ringKeys)
				slices.SortFunc(mortonKeys, func(a, z lattice.PackedCoord) int { return a.Compare(z) })

				for _, order := range []struct {
					name string
					keys []lattice.PackedCoord
				}{{"ring", ringKeys}, {"morton", mortonKeys}} {
					name := fmt.Sprintf("%s/%s/r%d/%s", cache.name, densityName, radius, order.name)
					b.Run(name, func(b *testing.B) {
						b.ReportMetric(float64(len(order.keys)), "lookups/op")
						b.ReportAllocs()
						b.ResetTimer()
						for b.Loop() {
							if err := db.View(func(tx *hexxladb.Tx) error {
								for _, key := range order.keys {
									if _, _, err := tx.GetCell(key); err != nil {
										return err
									}
								}
								return nil
							}); err != nil {
								b.Fatal(err)
							}
						}
					})
				}
			}
		}
	}
}

func benchPreloadSpatial(b *testing.B, center lattice.Coord, radius, stride int, opts *hexxladb.Options) *hexxladb.DB {
	b.Helper()
	db, err := hexxladb.Open(filepath.Join(b.TempDir(), "spatial.db"), opts)
	if err != nil {
		b.Fatal(err)
	}
	coords := lattice.WalkRings(nil, center, radius)
	if err := db.Update(func(tx *hexxladb.Tx) error {
		for i, coord := range coords {
			if i%stride != 0 {
				continue
			}
			key, err := lattice.Pack(coord)
			if err != nil {
				return err
			}
			if err := tx.PutCell(context.Background(), record.CellRecord{Key: key, RawContent: "spatial"}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		_ = db.Close()
		b.Fatal(err)
	}
	return db
}

// BenchmarkAPI_FindEdgePathDegree measures graph routing as out-degree grows.
// Every spoke must be expanded before the equal-cost goal, exercising one
// weighted adjacency scan per expanded coordinate.
func BenchmarkAPI_FindEdgePathDegree(b *testing.B) {
	for _, degree := range []int{8, 32, 128} {
		b.Run(fmt.Sprintf("degree_%d", degree), func(b *testing.B) {
			db, err := hexxladb.Open(filepath.Join(b.TempDir(), "path.db"), nil)
			if err != nil {
				b.Fatal(err)
			}
			b.Cleanup(func() { _ = db.Close() })
			start := lattice.Coord{}
			goal := lattice.Coord{Q: degree + 2, R: 0}
			if err := db.Update(func(tx *hexxladb.Tx) error {
				for i := range degree {
					spoke := lattice.Coord{Q: i + 1, R: 1}
					if err := tx.LinkCells(start, spoke, "route", 1, record.ProvenanceWire{}); err != nil {
						return err
					}
					if err := tx.LinkCells(spoke, goal, "route", 1, record.ProvenanceWire{}); err != nil {
						return err
					}
				}
				return nil
			}); err != nil {
				b.Fatal(err)
			}

			b.ReportMetric(float64(degree), "degree")
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if err := db.View(func(tx *hexxladb.Tx) error {
					_, err := tx.FindEdgePath(context.Background(), start, goal, hexxladb.FindEdgePathConfig{})
					return err
				}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkAPI_SuperHexRebuild measures a complete occupancy-index rebuild
// from the primary cell keyspace at representative hierarchy levels.
func BenchmarkAPI_SuperHexRebuild(b *testing.B) {
	for _, n := range apiBenchPreloadSizes(b) {
		for _, level := range []int{1, 3} {
			b.Run(fmt.Sprintf("cells_%d/level_%d", n, level), func(b *testing.B) {
				db, _ := benchAPIPreloadCellsWithOptions(b, n, &hexxladb.Options{
					ChangelogEnabled: true,
					ChangelogLazy:    true,
				})
				b.Cleanup(func() { _ = db.Close() })
				idx, err := hexxladb.NewSuperHexSummaryIndex(level)
				if err != nil {
					b.Fatal(err)
				}
				b.ReportMetric(float64(n), "cells/op")
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					if err := idx.Rebuild(context.Background(), db); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

// BenchmarkAPI_SuperHexSummaryForCoord measures the constant-time point lookup
// used by request paths after the derived index has been built.
func BenchmarkAPI_SuperHexSummaryForCoord(b *testing.B) {
	db, first := benchAPIPreloadCellsWithOptions(b, 2000, &hexxladb.Options{
		ChangelogEnabled: true,
		ChangelogLazy:    true,
	})
	b.Cleanup(func() { _ = db.Close() })
	coord, err := lattice.Unpack(first)
	if err != nil {
		b.Fatal(err)
	}
	idx, err := hexxladb.NewSuperHexSummaryIndex(2)
	if err != nil {
		b.Fatal(err)
	}
	if err := idx.Rebuild(context.Background(), db); err != nil {
		b.Fatal(err)
	}

	b.ReportMetric(2000, "cells")
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, ok, err := idx.SummaryForCoord(coord); err != nil || !ok {
			b.Fatalf("SummaryForCoord: ok=%v err=%v", ok, err)
		}
	}
}

// BenchmarkAPI_SuperHexSummaries measures deterministic export, including the
// parent-coordinate sort required by the public contract.
func BenchmarkAPI_SuperHexSummaries(b *testing.B) {
	db, _ := benchAPIPreloadCellsWithOptions(b, 2000, &hexxladb.Options{
		ChangelogEnabled: true,
		ChangelogLazy:    true,
	})
	b.Cleanup(func() { _ = db.Close() })
	idx, err := hexxladb.NewSuperHexSummaryIndex(2)
	if err != nil {
		b.Fatal(err)
	}
	if err := idx.Rebuild(context.Background(), db); err != nil {
		b.Fatal(err)
	}

	b.ReportMetric(float64(len(idx.Summaries())), "summaries/op")
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_ = idx.Summaries()
	}
}

// BenchmarkEvidence_SuperHexSyncCatchUp measures a fixed one-shot changelog
// catch-up. It is opt-in because every benchmark iteration constructs a fresh
// database to keep history length constant. Run with HEXXLA_SYNC_BENCH=1 and
// -benchtime=1x, as task evidence-controlled does.
func BenchmarkEvidence_SuperHexSyncCatchUp(b *testing.B) {
	if os.Getenv("HEXXLA_SYNC_BENCH") != "1" {
		b.Skip("set HEXXLA_SYNC_BENCH=1 and use -benchtime=1x")
	}
	for _, history := range []int{512, 2000} {
		for _, batchSize := range []int{1, 256} {
			b.Run(fmt.Sprintf("history_%d/batch_%d", history, batchSize), func(b *testing.B) {
				b.ReportMetric(float64(history), "history-records")
				b.ReportMetric(float64(batchSize), "records/op")
				b.ReportAllocs()
				for b.Loop() {
					b.StopTimer()
					db, _ := benchAPIPreloadCellsWithOptions(b, history, &hexxladb.Options{
						ChangelogEnabled: true,
						ChangelogLazy:    true,
					})
					idx, err := hexxladb.NewSuperHexSummaryIndex(2)
					if err != nil {
						b.Fatal(err)
					}
					if err := idx.Rebuild(context.Background(), db); err != nil {
						b.Fatal(err)
					}
					if err := db.Update(func(tx *hexxladb.Tx) error {
						for i := range batchSize {
							packed, err := lattice.Pack(lattice.Coord{Q: i % 200, R: i / 200})
							if err != nil {
								return err
							}
							if err := tx.PutCell(context.Background(), record.CellRecord{Key: packed, RawContent: "sync"}); err != nil {
								return err
							}
						}
						return nil
					}); err != nil {
						b.Fatal(err)
					}

					b.StartTimer()
					processed, err := idx.Sync(context.Background(), db, batchSize)
					b.StopTimer()
					if closeErr := db.Close(); closeErr != nil {
						b.Fatal(closeErr)
					}
					if err != nil {
						b.Fatal(err)
					}
					if processed != batchSize {
						b.Fatalf("processed=%d, want %d", processed, batchSize)
					}
					// B.Loop requires the timer to be running at the loop boundary.
					// Setup and validation remain excluded from the measured sample.
					b.StartTimer()
				}
			})
		}
	}
}
