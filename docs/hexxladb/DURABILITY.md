# Durability and redo

HexxlaDB uses a redo WAL beside the primary database. The database lock
serializes writers and excludes readers through commit and changefeed
finalization. Use one `DB.Update` for a group of records that should share a
durability boundary. See [transactions](./TX.md).

## Commit order

1. The engine stages page images and the final header in memory. Reads inside
   the write transaction see staged changes. Aborting the callback discards
   the staged state.
2. The engine writes the redo and commit header to the WAL and syncs it.
3. It writes the primary pages and syncs the primary, then publishes and syncs
   the header. The header carries the root, allocator boundary, redo watermark,
   and logical commit sequence. Header-only changes share the same path.
4. It resets the WAL for reuse. A later recovery skips records at or below the
   primary header's redo watermark.

Data, version visibility, allocator changes, and changefeed outbox intents share
this boundary. A failed commit can require close and reopen before writing
again. With changefeed enabled, projection and acknowledgment follow the
authoritative commit. A projection failure reports `ErrCommitDurable`.
See [commit errors](./TX.md#commit-finalization-failures).

`DB.WriteStats` reports time spent waiting, staging, committing, and finalizing.
The compatibility method `DB.GroupWALStats` reports commit attempts, zero
multi-job batches, and successful redo syncs. Its redo counter excludes the
sync for WAL extent cleanup. `Options.GroupWALMaxBatchWait` is deprecated and
ignored. `Options.UsePrimaryFdatasync` selects the primary file's data-only
barrier on platforms that support it.

## Recovery and version compatibility

`Open` validates the WAL in one streaming pass before writing to the
primary. A second pass applies pending records and publishes the validated
commit header. Recovery memory is proportional to a WAL record. Truncated,
invalid, or incomplete pending frames fail closed and preserve the recovery
set for inspection. See the [WAL format](../../internal/engine/ENGINE_FORMAT.md#wal-file).

- New v1/v2 WAL transactions begin with a header and a page-record count.
  Older versions can open a cleanly closed database. For an interrupted write
  with a pending new WAL, recover with the newer binary before using an older
  binary. The older reader rejects the pending header record before applying
  its pages.
- Old v2 WALs with pending page records and no commit header are refused.
  They cannot restore the logical sequence reliably. Keep the primary and WAL
  together and use a verified backup or a separately validated recovery process.
- V3 uses its existing authenticated trailing header marker. Old v1 page-only
  redo remains readable under the legacy recovery rules.

Database creation and physical backup also sync destination directory entries.
See [backup and copy](./OPERATIONS.md#backup-and-copy).

## Crash tests

The integration suite stops subprocesses at named phases, including WAL
append, WAL sync, primary write, primary sync, and header publication. Reopen
tests check visibility and commit sequences, a subsequent write, and
changefeed recovery. Separate tests exercise authenticated header-only
reclamation. Run `task integration`. See [commit crash tests](../../crash_ordering_integration_test.go)
and [reclamation crash tests](../../internal/engine/reclaim_integration_test.go).
