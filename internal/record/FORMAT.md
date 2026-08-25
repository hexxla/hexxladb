# Record envelope and v1 payloads

Binary records are **big-endian** numeric fields (`encoding/binary`). Strings are **UTF-8** with a **uint32** byte-length prefix (see `MaxStringField` in code).

## Envelope (all families)

| Offset | Size          | Field                        |
| ------ | ------------- | ---------------------------- |
| 0      | 4             | **magic** (ASCII per family) |
| 4      | 2             | **format_version** `uint16`  |
| 6      | 4             | **payload_len** `uint32`     |
| 10     | `payload_len` | **payload**                  |

Total record size = `10 + payload_len`.

**Version policy:** this release decodes **`format_version == 1`** only. **`format_version > 1`** → `ErrUnsupportedFormatVersion`. **`format_version < 1`** or **0** → `ErrUnknownFormatVersion`.

**Magics:** `HXCL` (cell), `HXFC` (facet), `HXED` (edge), `HXSM` (seam).

## PackedCoord (inside payloads)

Two `uint64` words in **big-endian**: **Lo** (`[0]`) then **Hi** (`[1]`), matching [internal/lattice/PACKED_COORD.md](../lattice/PACKED_COORD.md).

## Cell payload v1

After `Key` (`PackedCoord`): `RawContent` (str32), `Provenance` (see below), `Validity` (two optional int64: flag byte 0/1 + unix nano UTC), `Tags` (uint32 count, then str32 each), `ClusterHint` (uint8 0 absent, 1 + PackedCoord).

**Provenance:** `SourceID` str32, `Confidence` float64, `CreatedAt` int64 nano, `UpdatedAt` int64 nano.

## Facet payload v1

`PackedCoord`, `facet_id` byte 0–5, `DerivedContent` str32, `LastRotated` int64 unix nano, `DerivationHash` 32 raw SHA-256 bytes.

## Edge payload v1

`PackedCoord` from, `PackedCoord` to, `RelationType` str32, `Weight` float64, `Provenance` as above.

## Seam payload v1

16-byte **ULID** raw (`oklog/ulid`), then `CellA`/`CellB` `PackedCoord`, `SeamType` str32, `Reason` str32, `ConfidenceDelta` float64, `DetectedAt` int64 nano, `ResolutionStatus` str32, `ResolutionNote` str32, then optional **`Validity`** (same two optional-int64 encoding as **Cell**), then optional **`Provenance`** (same as **Cell** — `SourceID` str32, `Confidence` float64, `CreatedAt`/`UpdatedAt` int64 nano).

Records written before these suffixes existed end immediately after `ResolutionNote` (or after **`Validity`** only); decoders treat missing **`Validity`** as an open window; missing **`Provenance`** as empty.
