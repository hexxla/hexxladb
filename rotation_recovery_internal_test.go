package hexxladb

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/hexxla/hexxladb/internal/engine"
)

func TestRecoverInterruptedRotationRestoresOldFiles(t *testing.T) {
	t.Parallel()
	primary := filepath.Join(t.TempDir(), "database.db")
	changelog := filepath.Join(t.TempDir(), "audit.log")
	writeTestFile(t, primary, "new-primary")
	writeTestFile(t, primary+".rotate.bak", "old-primary")
	writeTestFile(t, primary+".rotate.tmp", "temporary-primary")
	writeTestFile(t, changelog, "new-changelog")
	writeTestFile(t, changelog+".rotate.bak", "old-changelog")
	writeTestFile(t, changelog+".rotate.tmp", "temporary-changelog")
	if err := writeRotationState(primary, rotationState{
		Version:       rotationStateVersion,
		ChangelogPath: changelog,
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := Open(primary, nil); !errors.Is(err, ErrRotationIncomplete) {
		t.Fatalf("Open error = %v, want ErrRotationIncomplete", err)
	}
	if err := RecoverInterruptedRotation(primary, nil); !errors.Is(err, ErrRotationIncomplete) {
		t.Fatalf("mismatched recovery error = %v, want ErrRotationIncomplete", err)
	}
	assertTestFileContent(t, primary, "new-primary")
	assertTestFileContent(t, primary+".rotate.bak", "old-primary")
	if err := RecoverInterruptedRotation(primary, &Options{
		ChangelogEnabled: true,
		ChangelogPath:    changelog,
	}); err != nil {
		t.Fatal(err)
	}
	assertTestFileContent(t, primary, "old-primary")
	assertTestFileContent(t, changelog, "old-changelog")
	for _, artifact := range []string{
		primary + ".rotate.bak",
		primary + ".rotate.tmp",
		primary + ".rotate.state",
		changelog + ".rotate.bak",
		changelog + ".rotate.tmp",
	} {
		if _, err := os.Lstat(artifact); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("rotation artifact %q remains: %v", artifact, err)
		}
	}
}

func TestRecoverInterruptedRotationBeforeSwapKeepsOriginal(t *testing.T) {
	t.Parallel()
	primary := filepath.Join(t.TempDir(), "database.db")
	writeTestFile(t, primary, "old-primary")
	if err := writeRotationState(primary, rotationState{Version: rotationStateVersion}); err != nil {
		t.Fatal(err)
	}
	if err := RecoverInterruptedRotation(primary, nil); err != nil {
		t.Fatal(err)
	}
	assertTestFileContent(t, primary, "old-primary")
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertTestFileContent(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("%s content = %q, want %q", path, got, want)
	}
}

func TestRotationPrepareClearsStaleBackupsBeforeRecoveryMarker(t *testing.T) {
	t.Parallel()
	for _, withChangelog := range []bool{false, true} {
		t.Run(fmt.Sprintf("changelog=%t", withChangelog), func(t *testing.T) {
			directory := t.TempDir()
			primary := filepath.Join(directory, "database.db")
			writeTestFile(t, primary, "current-primary-with-new-commits")
			writeTestFile(t, primary+".rotate.bak", "previous-rotation-primary")
			state := rotationState{Version: rotationStateVersion}
			var opts *Options
			if withChangelog {
				state.ChangelogPath = filepath.Join(directory, "audit.log")
				opts = &Options{ChangelogEnabled: true, ChangelogPath: state.ChangelogPath}
				writeTestFile(t, state.ChangelogPath, "current-changelog-with-new-commits")
				writeTestFile(t, state.ChangelogPath+".rotate.bak", "previous-rotation-changelog")
			}
			if err := prepareRotationState(primary, state); err != nil {
				t.Fatal(err)
			}
			// Crash immediately after the marker is durable, before any source rename.
			if err := RecoverInterruptedRotation(primary, opts); err != nil {
				t.Fatal(err)
			}
			assertTestFileContent(t, primary, "current-primary-with-new-commits")
			if withChangelog {
				assertTestFileContent(t, state.ChangelogPath, "current-changelog-with-new-commits")
			}
		})
	}
}

func TestRotationPrepareCleanupFailureDoesNotPublishRecoveryMarker(t *testing.T) {
	t.Parallel()
	primary := filepath.Join(t.TempDir(), "database.db")
	writeTestFile(t, primary, "current-primary")
	// A nonempty directory makes stale-backup cleanup fail on every platform.
	backupPath := primary + ".rotate.bak"
	if err := os.Mkdir(backupPath, 0o700); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(backupPath, "child"), "keep")
	if err := prepareRotationState(primary, rotationState{Version: rotationStateVersion}); err == nil {
		t.Fatal("prepare succeeded despite a stale-backup cleanup failure")
	}
	if pending, err := rotationPending(primary); err != nil || pending {
		t.Fatalf("rotation marker published on cleanup failure: pending=%t err=%v", pending, err)
	}
	assertTestFileContent(t, primary, "current-primary")
}

func TestRotationPathLockExcludesWritersAfterSourceClose(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "database.db")
	source, err := Open(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := source.Update(func(tx *Tx) error { return tx.Put([]byte("app/value"), []byte("committed")) }); err != nil {
		t.Fatal(err)
	}
	if err := source.Close(); err != nil {
		t.Fatal(err)
	}
	newOpts := &Options{EncryptionKey: make([]byte, 32)}
	checked := false
	err = rotateEncryptionWithOptions(path, nil, newOpts, nil, func() {
		checked = true
		// This is the exact former gap: the primary lock has been released,
		// and no recovery marker has been written yet.
		if pending, err := rotationPending(path); err != nil || pending {
			t.Fatalf("unexpected rotation marker before swap: pending=%t err=%v", pending, err)
		}
		writer, err := OpenExisting(path, nil)
		if writer != nil {
			_ = writer.Close()
		}
		if !errors.Is(err, ErrDatabaseLocked) {
			t.Errorf("writer entered rotation gap: %v", err)
		}
		if err := RecoverInterruptedRotation(path, nil); !errors.Is(err, ErrDatabaseLocked) {
			t.Errorf("recovery entered live rotation: %v", err)
		}
		if err := RotateEncryption(path, nil, newOpts); !errors.Is(err, ErrDatabaseLocked) {
			t.Errorf("second rotation entered live rotation: %v", err)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if !checked {
		t.Fatal("source-close boundary was not checked")
	}
	rotated, err := OpenExisting(path, newOpts)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rotated.Close() }()
	if err := rotated.View(func(tx *Tx) error {
		value, found, err := tx.Get([]byte("app/value"))
		if err == nil && (!found || string(value) != "committed") {
			t.Errorf("rotated value=%q found=%t", value, found)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func TestRotationRecoveryPathLockPreservesPendingFiles(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "database.db")
	writeTestFile(t, path, "new-primary")
	writeTestFile(t, path+".rotate.bak", "old-primary")
	if err := writeRotationState(path, rotationState{Version: rotationStateVersion}); err != nil {
		t.Fatal(err)
	}
	lock, err := engine.AcquirePathLock(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lock.Close() }()
	if err := RecoverInterruptedRotation(path, nil); !errors.Is(err, ErrDatabaseLocked) {
		t.Fatalf("recovery ignored path lock: %v", err)
	}
	assertTestFileContent(t, path, "new-primary")
	assertTestFileContent(t, path+".rotate.bak", "old-primary")
	if pending, err := rotationPending(path); err != nil || !pending {
		t.Fatalf("recovery changed marker while locked: pending=%t err=%v", pending, err)
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
	if err := RecoverInterruptedRotation(path, nil); err != nil {
		t.Fatal(err)
	}
	assertTestFileContent(t, path, "old-primary")
	if _, err := os.Stat(path + ".lock"); err != nil {
		t.Fatalf("recovery removed stable lock inode: %v", err)
	}
}
