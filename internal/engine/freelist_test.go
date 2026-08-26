package engine

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestAuthenticatedFreelistReusesPagesAcrossReopen(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "reuse.db")
	opts := authenticatedRecoveryTestOptions()
	opts.MaxValueBytes = 32 << 10
	value := incompressibleTestValue(20 << 10)

	e, err := Open(path, opts)
	if err != nil {
		t.Fatal(err)
	}
	tree := OpenBTree(e)
	commitTreeMutation(t, e, func() error { return tree.Put([]byte("large"), value) })
	populated, err := e.ReadHeader()
	if err != nil {
		t.Fatal(err)
	}
	commitTreeMutation(t, e, func() error { return tree.Delete([]byte("large")) })
	freed, err := e.ReadHeader()
	if err != nil {
		t.Fatal(err)
	}
	if freed.FreelistCount == 0 || freed.BTreeRoot != 0 {
		t.Fatalf("header after delete = %+v, want free pages and empty tree", freed)
	}
	if err := e.Close(); err != nil {
		t.Fatal(err)
	}

	e, err = Open(path, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = e.Close() }()
	tree = OpenBTree(e)
	commitTreeMutation(t, e, func() error { return tree.Put([]byte("replacement"), value) })
	reused, err := e.ReadHeader()
	if err != nil {
		t.Fatal(err)
	}
	if reused.NextPageID != populated.NextPageID {
		t.Fatalf("next page id grew from %d to %d despite reusable pages", populated.NextPageID, reused.NextPageID)
	}
	got, ok, err := tree.Get([]byte("replacement"))
	if err != nil || !ok || !bytes.Equal(got, value) {
		t.Fatalf("replacement read ok=%v err=%v len=%d", ok, err, len(got))
	}
}

func TestAuthenticatedReclaimTailPublishesBeforeTruncate(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "reclaim.db")
	opts := authenticatedRecoveryTestOptions()
	opts.MaxValueBytes = 32 << 10
	e, err := Open(path, opts)
	if err != nil {
		t.Fatal(err)
	}
	tree := OpenBTree(e)
	value := incompressibleTestValue(20 << 10)
	commitTreeMutation(t, e, func() error { return tree.Put([]byte("large"), value) })
	commitTreeMutation(t, e, func() error { return tree.Delete([]byte("large")) })
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	reclaimed, err := e.ReclaimTail()
	if err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if reclaimed == 0 || before.Size()-after.Size() != int64(reclaimed) {
		t.Fatalf("sizes %d -> %d, reclaimed %d", before.Size(), after.Size(), reclaimed)
	}
	hdr, err := e.ReadHeader()
	if err != nil {
		t.Fatal(err)
	}
	if hdr.NextPageID != 1 || hdr.FreelistCount != 0 || after.Size() != int64(hdr.PageSize) {
		t.Fatalf("header=%+v size=%d, want empty single-page primary", hdr, after.Size())
	}
	// Model interruption after allocator publication but before truncation: an
	// excess physical suffix is harmless and the next call removes it.
	if err := e.db.Truncate(after.Size() + int64(e.physicalPageSize)); err != nil {
		t.Fatal(err)
	}
	if reclaimed, err := e.ReclaimTail(); err != nil || reclaimed != uint64(e.physicalPageSize) {
		t.Fatalf("retry reclaim = %d, %v", reclaimed, err)
	}
	reconciled, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if reconciled.Size() != after.Size() {
		t.Fatalf("reconciled size = %d, want %d", reconciled.Size(), after.Size())
	}
	if err := e.Close(); err != nil {
		t.Fatal(err)
	}
	e, err = Open(path, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = e.Close() }()
	commitTreeMutation(t, e, func() error { return OpenBTree(e).Put([]byte("new"), []byte("value")) })
}

func TestAuthenticatedFreelistAbortDoesNotPublishRelease(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "abort.db")
	e, err := Open(path, authenticatedRecoveryTestOptions())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = e.Close() }()
	tree := OpenBTree(e)
	commitTreeMutation(t, e, func() error { return tree.Put([]byte("key"), []byte("value")) })
	before, err := e.ReadHeader()
	if err != nil {
		t.Fatal(err)
	}
	if err := e.BeginWriteTxn(); err != nil {
		t.Fatal(err)
	}
	if err := tree.Delete([]byte("key")); err != nil {
		t.Fatal(err)
	}
	e.AbortWriteTxn()
	after, err := e.ReadHeader()
	if err != nil {
		t.Fatal(err)
	}
	if after.FreelistCount != before.FreelistCount || after.BTreeRoot != before.BTreeRoot {
		t.Fatalf("abort published allocator/tree state: before=%+v after=%+v", before, after)
	}
	if got, ok, err := tree.Get([]byte("key")); err != nil || !ok || string(got) != "value" {
		t.Fatalf("get after abort = %q, %v, %v", got, ok, err)
	}
}

func TestAuthenticatedReusedPageIgnoresStaleWAL(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "stale-wal.db")
	opts := authenticatedRecoveryTestOptions()
	e, err := Open(path, opts)
	if err != nil {
		t.Fatal(err)
	}
	tree := OpenBTree(e)
	commitTreeMutation(t, e, func() error { return tree.Put([]byte("old"), []byte("old-value")) })
	oldHeader, err := e.ReadHeader()
	if err != nil {
		t.Fatal(err)
	}
	staleWAL, err := os.ReadFile(WalPath(path)) // #nosec G304 -- test-owned path.
	if err != nil {
		t.Fatal(err)
	}
	commitTreeMutation(t, e, func() error { return tree.Delete([]byte("old")) })
	commitTreeMutation(t, e, func() error { return tree.Put([]byte("new"), []byte("new-value")) })
	currentHeader, err := e.ReadHeader()
	if err != nil {
		t.Fatal(err)
	}
	if currentHeader.BTreeRoot != oldHeader.BTreeRoot || currentHeader.LastWALSeq <= oldHeader.LastWALSeq {
		t.Fatalf("page was not reused with a newer commit: old=%+v current=%+v", oldHeader, currentHeader)
	}
	if err := e.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(WalPath(path), staleWAL, 0o600); err != nil {
		t.Fatal(err)
	}
	e, err = Open(path, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = e.Close() }()
	got, ok, err := OpenBTree(e).Get([]byte("new"))
	if err != nil || !ok || string(got) != "new-value" {
		t.Fatalf("new value after stale WAL = %q, %v, %v", got, ok, err)
	}
}

func TestAuthenticatedFreelistExternalPagesSurviveReopen(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "external.db")
	opts := authenticatedRecoveryTestOptions()
	opts.MaxValueBytes = 512 << 10
	e, err := Open(path, opts)
	if err != nil {
		t.Fatal(err)
	}
	tree := OpenBTree(e)
	value := incompressibleTestValue(256 << 10)
	commitTreeMutation(t, e, func() error { return tree.Put([]byte("large"), value) })
	commitTreeMutation(t, e, func() error { return tree.Delete([]byte("large")) })
	hdr, err := e.ReadHeader()
	if err != nil {
		t.Fatal(err)
	}
	if hdr.FreelistCount <= HeaderInlineFreelistCapacity || hdr.FreelistHead == 0 || hdr.FreelistHeadGeneration == 0 {
		t.Fatalf("header = %+v, want external freelist", hdr)
	}
	if err := e.Close(); err != nil {
		t.Fatal(err)
	}
	e, err = Open(path, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = e.Close() }()
	commitTreeMutation(t, e, func() error { return OpenBTree(e).Put([]byte("again"), value) })
	after, err := e.ReadHeader()
	if err != nil {
		t.Fatal(err)
	}
	if after.NextPageID > hdr.NextPageID+1 {
		t.Fatalf("next page id grew from %d to %d beyond one-time metadata overhead", hdr.NextPageID, after.NextPageID)
	}
	commitTreeMutation(t, e, func() error { return OpenBTree(e).Delete([]byte("again")) })
	steady, err := e.ReadHeader()
	if err != nil {
		t.Fatal(err)
	}
	commitTreeMutation(t, e, func() error { return OpenBTree(e).Put([]byte("steady"), value) })
	final, err := e.ReadHeader()
	if err != nil {
		t.Fatal(err)
	}
	if final.NextPageID != steady.NextPageID {
		t.Fatalf("steady-state next page id grew from %d to %d", steady.NextPageID, final.NextPageID)
	}
}

func commitTreeMutation(t testing.TB, e *Engine, mutate func() error) {
	t.Helper()
	if err := e.BeginWriteTxn(); err != nil {
		t.Fatal(err)
	}
	if err := mutate(); err != nil {
		e.AbortWriteTxn()
		t.Fatal(err)
	}
	if err := e.CommitWriteTxn(); err != nil {
		t.Fatal(err)
	}
}

func incompressibleTestValue(size int) []byte {
	value := make([]byte, size)
	var state uint64 = 0x9e3779b97f4a7c15
	for i := range value {
		state ^= state << 13
		state ^= state >> 7
		state ^= state << 17
		value[i] = byte(state)
	}
	return value
}
