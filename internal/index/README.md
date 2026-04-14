# `internal/index`

Storage **key encoding** for the embedded engine: canonical byte prefixes and **`PackedCoord`** serialization so **lexicographic order** matches **[`lattice.PackedCoord.Compare`](../../internal/lattice/packed.go)** (required for Morton-aligned range scans).

- **`cell/`** primary keys — [`cell_key.go`](./cell_key.go), **[HEXXLA_DB.md](../../docs/hexxladb/HEXXLA_DB.md)** (`cell/<packed_coord>`).

Milestone **M4** adds cell keys; facet/edge/seam encodings follow in later milestones.
