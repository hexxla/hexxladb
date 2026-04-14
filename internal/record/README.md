# `internal/record`

Versioned **binary envelopes** and **v1 codecs** for **Cell**, **Facet**, **Edge**, and **Seam** blobs (see [HEXXLA_DB.md](../../docs/hexxladb/HEXXLA_DB.md)).

- **Layout:** [FORMAT.md](./FORMAT.md) (design gate — bump `format_version` + engine format if incompatible).
- **Milestone:** [DEVELOPMENT_ROADMAP.md](../../docs/hexxladb/DEVELOPMENT_ROADMAP.md) **M2**.
- **Dependency:** [oklog/ulid](https://github.com/oklog/ulid) v2 for seam IDs.

No filesystem or engine I/O here — encode/decode only.
