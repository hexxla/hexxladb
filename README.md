<div align="center">

<img src="assets/images/hexxladb_logo_shadow.svg" alt="HexxlaDB" width="240">

# HexxlaDB

**An embedded Go database for topic neighborhoods, bounded retrieval, and explicit revisions.**

[![CI](https://github.com/hexxla/hexxladb/actions/workflows/ci.yml/badge.svg)](https://github.com/hexxla/hexxladb/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/hexxla/hexxladb.svg)](https://pkg.go.dev/github.com/hexxla/hexxladb)
[![Version](https://img.shields.io/github/v/tag/hexxla/hexxladb?label=version&color=7c3aed)](https://github.com/hexxla/hexxladb/tags)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

</div>

**Status: alpha.** This repository and the HexxlaDB–Mosaic–MCP Ratchet stack
are under active development. Use controlled evaluation with recoverable data;
general production sign-off has not been given. Existing tags remain intact.
[Release policy and beta/v1 gates](VERSIONING.md)

HexxlaDB stores records as cells on a sparse hex grid. Search by text, tags,
or application-supplied vectors, then expand around the results to find nearby
or linked context. Each cell can carry provenance, confidence, and a validity
window. Seams record contradictions and supersession between cells. The library
stores and queries records directly in your Go process.

## Group records by topic

Give each topic an anchor coordinate and place new cells within a bounded
radius. For example, put **Backups** at `(0,0)` and **Migration** at `(10,0)`,
with two placement rings per topic. Cells from the two groups are at least
six hex steps apart, so the other group lies outside a three-ring radial
scan from any cell in either group. Tags let you find topic records from
anywhere on the grid.

Your application chooses the anchors, radii, and tags. Use
`FindUnusedCellPlacement` and `PutCell` in the same `DB.Update` to allocate
a new record without reusing a recorded tombstone. Graph edges and supersession
can deliberately connect records across neighborhoods.
[Placement and coordinate guide](docs/hexxladb/API_REFERENCE.md#coordinates-and-records)

## Quick start

### Run the example from a checkout

Use the Go version in [`go.mod`](go.mod) or newer.

```sh
git clone https://github.com/hexxla/hexxladb.git
cd hexxladb
go run ./examples/quickstart
```

The [quickstart](examples/quickstart/) creates a temporary database, places two
records, finds a seed with a bounded lexical query, and follows a `requires`
edge to assemble context. It renders a sample prompt with an application-owned
word budget. With Task installed, `task demo` runs the same example.

### Install the released library

From your own Go module, install the current alpha candidate.

```sh
go get github.com/hexxla/hexxladb@v0.8.0-alpha.1
```

Use the [tagged README](https://github.com/hexxla/hexxladb/blob/v0.8.0-alpha.1/README.md)
for that version. The example and explorer commands on this page run the
current checkout.

## Put and fetch a cell

This complete Go program opens `memory.db` (creating it if absent), stores a
cell at axial coordinate `(0,0)`, and fetches it. Default opens use MVCC.
One database handle owns the files; use `DB.Update` for writes and `DB.View`
for consistent snapshot reads.

```go
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/hexxla/hexxladb"
)

func main() {
	if err := run(context.Background()); err != nil {
		slog.Error("database example failed", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) (err error) {
	db, err := hexxladb.Open("memory.db", nil)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, db.Close()) }()

	key, err := hexxladb.Pack(hexxladb.Coord{Q: 0, R: 0})
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	cell := hexxladb.CellRecord{
		Key:        key,
		RawContent: "Verify each backup by restoring it in staging.",
		Tags:       []string{"operations", "backups"},
		Provenance: hexxladb.ProvenanceWire{
			SourceID:   "runbook:backups/v3#restore",
			Confidence: 0.95,
			CreatedAt:  now.UnixNano(),
			UpdatedAt:  now.UnixNano(),
		},
		Validity: hexxladb.ValidityWire{
			ValidFrom: new(now.UnixNano()),
			ValidTo:   new(now.AddDate(0, 3, 0).UnixNano()),
		},
		ClusterHint: new(key), // This cell is its own topic anchor.
	}
	if err := db.Update(func(tx *hexxladb.Tx) error {
		return tx.PutCell(ctx, cell)
	}); err != nil {
		return err
	}

	return db.View(func(tx *hexxladb.Tx) error {
		stored, found, err := tx.GetCell(key)
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("cell missing after commit")
		}
		fmt.Printf("%s\nsource=%s confidence=%.2f tags=%v\n",
			stored.RawContent, stored.Provenance.SourceID,
			stored.Provenance.Confidence, stored.Tags)
		return nil
	})
}
```

Output:

```text
Verify each backup by restoring it in staging.
source=runbook:backups/v3#restore confidence=0.95 tags=[operations backups]
```

`GetCell` returns `found=false` with no error for a missing cell. `PutCell`
creates a version at the supplied coordinate, replacing its current value if
one exists. Re-running the program updates `(0,0)`; use the placement helpers
[above](#group-records-by-topic) when allocating a new identity.

### What a cell stores

| Field | Example / meaning |
| --- | --- |
| `Key` | Packed hex coordinate, constructed with `Pack`; the cell's identity. |
| `RawContent` | Text, or application-encoded content such as a JSON string. |
| `Tags` | Application labels used for filtering and retrieval. |
| `Provenance.SourceID` | An application-chosen source reference, such as a document/version/section or conversation ID. |
| `Provenance.Confidence` | A finite, application-defined confidence signal; this example uses 0–1. |
| `Provenance.CreatedAt`, `UpdatedAt` | Caller-supplied record timestamps, as UTC Unix nanoseconds. |
| `Validity.ValidFrom`, `ValidTo` | Optional half-open validity window `[from, to)`; `nil` leaves that end unbounded. |
| `ClusterHint` | Optional packed topic-anchor coordinate; stored guidance, not enforced clustering. |

Provenance is caller-supplied attribution, not verified authorship. The native
record has one source reference and no arbitrary metadata map. Encode richer
application data in `RawContent` or keep it in an application-owned store linked
by `SourceID`. JSON content is stored as a string, not indexed as JSON fields.
Exact `GetCell` reads the visible record; it does not automatically hide records
outside their validity window. MVCC revision history and the application validity
window are separate concepts.

For shorter construction, `NewFactCell(key, content, sourceID, "operations", 0.95)`
sets provenance timestamps and `fact`/category tags. User-message and
assistant-response helpers are also available. Embeddings, derived facets,
graph edges and contradiction/supersession seams are separate records associated
with coordinates; they are not additional fields inside `CellRecord`.
[Records and metadata](docs/hexxladb/API_REFERENCE.md#coordinates-and-records)

## More common operations

These snippets use the `db`, `ctx` and `key` from `run` above while the database
is still open. Keep related changes inside one `DB.Update` for an atomic commit;
return callback errors so failed updates abort.

### Search by text and tags

```go
if err := db.View(func(tx *hexxladb.Tx) error {
	hits, err := tx.QueryCells(ctx, hexxladb.CellQuery{
		Query: "backup", RequireTags: []string{"operations"},
		MaxResults: 10, MaxScanRows: 1000,
	})
	if err != nil && !errors.Is(err, hexxladb.ErrQueryScanLimit) {
		return err
	}
	for _, hit := range hits {
		fmt.Println(hit.Cell.Coord, hit.Cell.RawContent)
	}
	if errors.Is(err, hexxladb.ErrQueryScanLimit) {
		fmt.Println("Scan budget reached; results are partial.")
	}
	return nil
}); err != nil {
	return err
}
```

### Update a cell without losing its metadata

Read and modify within the same write transaction. Preserve creation time and
set the updated timestamp explicitly; `PutCell` does not supply these for you.
If you maintain embeddings, replace or remove the vector in the same transaction
when its content changes; the native library does not generate embeddings.

```go
if err := db.Update(func(tx *hexxladb.Tx) error {
	cell, found, err := tx.GetCell(key)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("cannot update a missing cell")
	}
	cell.RawContent = "Restore each backup in staging and verify its checksum."
	cell.Provenance.UpdatedAt = time.Now().UTC().UnixNano()
	return tx.PutCell(ctx, cell)
}); err != nil {
	return err
}
```

### Store another cell and link it atomically

Edges have their own relation type, weight and provenance. Here the backup
instruction at `(0,0)` requires the validation step at `(1,0)`.

```go
next, err := hexxladb.Pack(hexxladb.Coord{Q: 1, R: 0})
if err != nil {
	return err
}
if err := db.Update(func(tx *hexxladb.Tx) error {
	step := hexxladb.NewFactCell(next, "Compare the restored checksum.",
		"runbook:backups/v3#checksum", "operations", 1)
	if err := tx.PutCell(ctx, step); err != nil {
		return err
	}
	return tx.LinkCells(hexxladb.Coord{}, hexxladb.Coord{Q: 1, R: 0},
		"requires", 1, hexxladb.NewProvenanceWire("runbook:backups/v3", 1))
}); err != nil {
	return err
}
```

### Delete a cell

```go
if err := db.Update(func(tx *hexxladb.Tx) error {
	return tx.DeleteCell(ctx, key)
}); err != nil {
	return err
}
```

A subsequent current-snapshot `GetCell(key)` returns `found=false`. Retained
MVCC snapshots can still expose earlier cell versions until pruning. Deletion
is not a history purge or a file compaction; see the
[deletion contract](docs/hexxladb/API_REFERENCE.md#cells-facets-edges-and-seams).

## Choose how to retrieve context

- **Find seeds.** `QueryCells` supports lexical search, structured filters, and
  optional vector similarity. Filtered HNSW search is approximate.
  `QueryCellsWithStats` reports when the candidate budget ends the search.
  For exact scoring over a bounded scan, set `MaxScanRows` with a required
  tag, source, or radius. Row-limited scans can return partial hits with
  `ErrQueryScanLimit`.
  [Query guide](docs/hexxladb/API_REFERENCE.md#query-and-content-search)
- **Expand from a seed.** `LoadContext` scans neighboring rings or follows
  selected edge types. It can include facets and seams and resolve supersession.
  Result and traversal limits bound database work. Applications own prompt
  ranking and model-specific token accounting.
  [Context guide](docs/hexxladb/API_REFERENCE.md#context-assembly-and-spatial-retrieval)
- **Use visibility when space matters.** `LoadContextFOV` selects visible
  cells in all directions around a seed using the caller's opacity rule.
  [Spatial retrieval guide](docs/hexxladb/API_REFERENCE.md#specialized-retrieval)

## Terminal explorer

From the checkout with Task installed, create the synthetic fixture once and
open it.

```sh
task seed
task tui
```

- **Cells.** Press `/` to search, then Enter on a result to inspect it.
- **Inspector.** Press `c` for a three-ring radial context pack or `d` for
  cell details. Scroll with the arrows or Page Up and Page Down. Press Esc to return.
- **Hex Grid.** Press `2` to open the grid and `v` to show visibility from
  its center. The explorer treats empty hexes as obstacles.

Use Tab or `1`–`5` to switch views and `?` for contextual shortcuts.
Open an existing database with `task tui TUI_DB=/path/to/existing.db`.
The explorer takes the exclusive database lock. Its optional semantic and
hybrid search modes use local Ollama. See the
[operator guide](docs/hexxladb/OPERATIONS.md#terminal-explorer) for credentials,
fixture paths, and diagnostics.

## Examples and documentation

- [Ports and adapters](examples/ports_adapters/) demonstrates recording and
  resolving a contradiction through an application-owned port.
- [Remote access](examples/remote_access/) demonstrates an authenticated
  loopback service that owns the database files.
- [API guide](docs/hexxladb/API_REFERENCE.md) covers records, search, graph
  traversal, and context assembly. The [memory model](docs/hexxladb/HEXXLA.md)
  explains placement, validity, and contradictions.
- [Operations](docs/hexxladb/OPERATIONS.md) covers backups, migrations,
  compaction, and diagnostics. See also the [encryption model](docs/hexxladb/ENCRYPTION.md).
- [Performance evidence](docs/hexxladb/PERFORMANCE_EVIDENCE.md) records dated
  benchmarks and the workloads under [`tools/evidence`](tools/evidence/).
  Use `task bench-api` to measure the current checkout. Commits are serialized.
  Batch related writes in `DB.Update` to share one durable commit.
- [Compatibility policy](VERSIONING.md) and [roadmap](docs/ROADMAP.md) describe
  upgrade expectations and planned work.

`task build` builds the CLI and TUI. `task build-examples` compiles the examples.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for setup and checks. Run `task ci`
before pushing. Report bugs in [Issues](https://github.com/hexxla/hexxladb/issues)
or share ideas in [Discussions](https://github.com/hexxla/hexxladb/discussions).

Used by [Mosaic](https://github.com/hexxla/mosaic) and the
[Hexxla stack](https://github.com/hexxla/hexxla).
