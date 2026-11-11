# Durability and redo

HexxlaDB uses a redo WAL beside the primary database. The database lock
serializes writers and excludes readers through commit and changefeed
finalization. Use one `DB.Update` for a group of records that should share a
durability boundary. See [transactions](./TX.md).

## Commit order

1. The engine stages each page's final image and the final header in memory. Reads inside
   the write transaction see staged changes. Aborting the callback discards
   the staged state.
2. The engine writes the redo and commit header to the WAL and syncs it.
   The commit descriptor binds the count and ordered digest of page sequences,
   routing IDs, and stored images.
3. It writes the primary pages and syncs the primary, then publishes and syncs
   the header. The header carries the root, allocator boundary, redo watermark,
   and logical commit sequence. Header-only changes share the same path.
4. It resets the WAL for reuse. Recovery validates complete commit framing
   before relying on the primary header's redo watermark.

When a commit's WAL extent already ends at its final written byte, the earlier
WAL sync covers the complete extent. Cleanup truncation and its sync apply
only when replacing a longer previous extent.

Data, version visibility, allocator changes, and changefeed outbox intents share
this boundary. An engine commit failure fences storage reads and writes until
close and reopen. With changefeed enabled, projection and acknowledgment follow the
authoritative commit. A projection failure reports `ErrCommitDurable`.
See the [finalization recovery runbook](./CHANGEFEED.md#finalization-recovery-runbook).

`DB.WriteStats` reports time spent waiting, staging, committing, and finalizing.
`DB.WALStats` reports commit attempts and successful redo syncs. Redo syncs exclude the sync for WAL extent cleanup. Batch records in a single `DB.Update` to share a commit.
`Options.UsePrimaryFdatasync` selects the primary file’s data-only barrier on
platforms that support it.

## Recovery and version compatibility

`Open` validates the WAL in one streaming pass before writing to the
primary. A second pass applies pending records and publishes the validated
commit header. Recovery memory is proportional to a WAL record. Truncated,
invalid, or incomplete pending frames fail closed and preserve the recovery
set for inspection. See the [WAL format](../../internal/engine/ENGINE_FORMAT.md#wal-file).

Current commits require count-and-digest framing through a persisted feature
bit. Plaintext v2 headers also carry a checksum. If the mutable primary header
tears, recovery can repair it from a complete framed WAL using the primary's
unchanged format, page size, and encryption identity. Damage to that bootstrap
identity requires a verified recovery set. Checksums in plaintext files detect
accidental corruption and provide no keyed authenticity.

- New v2 WAL transactions begin with a header and a page-record count.
  Existing clean databases and previously supported marked WALs remain
  readable. After a current commit records the framing feature, use a
  pre-upgrade recovery-set backup to return to an older writer. Older writers
  cannot maintain the new framing and plaintext-header checksum.
- Old v2 WALs with pending page records and no commit header are refused.
  They cannot restore the logical sequence reliably. Keep the primary and WAL
  together and use a verified backup or a separately validated recovery process.
- V3 uses its existing authenticated trailing header marker. Old v1 page-only
  redo requires its original release for recovery.

Reopen-time persisted configuration changes use the same header-only commit
path after all supplied configuration has been validated.

Database creation and physical backup also sync destination directory entries.
See [backup and copy](./OPERATIONS.md#backup-and-copy).

## Crash tests

The integration suite stops subprocesses at named phases, including WAL
append, WAL sync, primary write, primary sync, and header publication. Reopen
tests check visibility and commit sequences, a subsequent write, and
changefeed recovery. Separate tests exercise authenticated header-only
reclamation. Run `task integration`. See [commit crash tests](../../crash_ordering_integration_test.go)
and [reclamation crash tests](../../internal/engine/reclaim_integration_test.go).

Deterministic fixtures also mix old and new mutable header bytes, remove or
misroute WAL frames, and validate recovery-set preservation on rejection.
Process interruption tests and torn-write fixtures cover distinct failures.
