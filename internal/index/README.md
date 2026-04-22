# `internal/index`

Storage **key encoding** for the embedded engine: canonical byte prefixes and **`PackedCoord`** serialization so **lexicographic order** matches **[`lattice.PackedCoord.Compare`](../../internal/lattice/packed.go)** (required for Morton-aligned range scans).

- **`cell/`** primary keys — [`cell_key.go`](./cell_key.go), **[HEXXLA_DB.md](../../docs/hexxladb/HEXXLA_DB.md)** (`cell/<packed_coord>`).
- **`tag/`** secondary keys for cells — [`tag_key.go`](./tag_key.go) (`tag/<tag>/<packed_coord>`, MVCC suffix parity with other secondaries).

Additional families (`facet/`, `edge/`, seam keys, etc.) live alongside these files — see **`HEXXLA_DB.md`** storage layout.
