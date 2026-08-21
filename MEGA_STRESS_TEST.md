# MEGA Stress Test Plan for HexxlaDB

## Current Test Coverage

| Test Type        | Status     | Notes                                                                                                                                                                                                                                                      |
| ---------------- | ---------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Unit tests       | ✅ Good    | Race detector enabled                                                                                                                                                                                                                                      |
| Crash tests      | ⚠️ Limited | SIGKILL at 5 phases only                                                                                                                                                                                                                                   |
| Stress tests     | ⚠️ Limited | 100K-500K cells max                                                                                                                                                                                                                                        |
| Fuzz tests       | ⚠️ Basic   | Header + WAL decode only                                                                                                                                                                                                                                   |
| Corruption tests | ⚠️ Partial | B+ tree cascade/split integrity covered: `TestProbe_*`, `TestCascadeIntegrity_*` (tree validator: balance, parent linkage, page-fit, top-down reachability, reopen) + `TestProbe_CompressMagicCollision`. Page-level byte corruption injection still TODO. |

## MEGA Stress Test Checklist

### Phase 1: B-Tree Corruption Injection

Create `internal/engine/corruption_test.go`:

```go
// TestCorruptHeaderVersion - corrupt format version in header
// TestCorruptRootPageCount - corrupt key count in root
// TestCorruptLeafSiblingPtr - corrupt sibling pointer (isolation violation)
// TestCorruptInternalPagePtr - point to non-existent page
// TestCorruptOverflowChain - break overflow page chain
// TestCorruptKeyOrder - keys out of order (invariant violation)
```

### Phase 2: WAL Corruption

```go
// TestCorruptWALChecksum - verify checksum validation
// TestCorruptWALSequence - out-of-order sequence numbers
// TestCorruptWALPayload - partial record corruption
// TestTornWALWrite - half-written WAL record
```

### Phase 3: Concurrent Chaos

```go
// TestConcurrentChaos - 100 writers + readers + random SIGKILL
// TestInterleavedCommits - overlapping long-running transactions
// TestRingDensityStress - 1M cells + WalkRing queries
// TestHNSWGraphStress - 100K embeddings + concurrent search
```

### Phase 4: Resource Exhaustion

```go
// TestDiskFullBehavior - simulate ENOSPC during write
// TestMemoryPressure - limit RSS, trigger OOM
// TestFileDescriptorExhaustion - simulate EMFILE
// TestSlowDisk - inject latency into fsync
```

## Implementation Priority

1. **HIGH**: B-tree page corruption injection framework
2. **HIGH**: WAL corruption scenarios
3. **MEDIUM**: Concurrent chaos test (100 concurrent ops)
4. **MEDIUM**: 10M cell stress test
5. **LOW**: Resource exhaustion tests

## Running MEGA Stress Tests

```bash
# All MEGA tests
task mega-stress

# Specific corruption tests
go test -tags=corruption -run TestCorrupt ./internal/engine/...

# Chaos test (10 min, concurrent writers)
HEXXLA_CHAOS_DURATION=10m go test -tags=chaos -v ./...

# 10M cell stress
HEXXLA_STRESS_CELLS=10000000 go test -tags=stress -run TestStress ./...
```

## Corruption Test Harness Design

```go
type CorruptionTest struct {
    DBPath string

    // Corruption point
    AtPhase string      // "wal_appended", "wal_synced", "primary_written", etc.
    AtPageID uint64     // which page to corrupt
    AtOffset int        // byte offset within page
    AtValue byte        // value to write
}

func (ct *CorruptionTest) Run(t *testing.T) {
    // 1. Create database with data
    // 2. Block at AtPhase (using crashtest hooks)
    // 3. Apply corruption
    // 4. Resume/terminate
    // 5. Reopen and verify consistency
    // 6. Check for panic, data loss, or graceful error
}
```
