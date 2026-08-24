//go:build integration

package hexxladb_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/hexxla/hexxladb"
)

// TestIntegration_crashChild runs in a forked [exec.Command] process; it is only meaningful when
// HEXXLADB_IN_CHILD=1. It performs one [DB.Update] that should block in the engine at a
// [crashtest.At] phase until the parent sends SIGKILL.
func TestIntegration_crashChild(t *testing.T) {
	if os.Getenv("HEXXLADB_IN_CHILD") != "1" {
		t.Skip("HEXXLADB_IN_CHILD=1 only")
	}
	if runtime.GOOS == "windows" {
		t.Skip("Unix-only subprocess test")
	}
	path := os.Getenv("HEXXLADB_TEST_DB_PATH")
	if path == "" {
		t.Fatal("set HEXXLADB_TEST_DB_PATH")
	}
	var opts *hexxladb.Options
	if os.Getenv("HEXXLADB_TEST_CHANGEFEED") == "1" {
		opts = &hexxladb.Options{EnableMVCC: true, ChangelogEnabled: true}
	}
	db, err := hexxladb.Open(path, opts)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = db.Close() }()
	if err := db.Update(func(tx *hexxladb.Tx) error {
		if opts == nil {
			return tx.Put([]byte("crash_key"), []byte("crash_val"))
		}
		coord, packErr := hexxladb.Pack(hexxladb.Coord{Q: 4, R: -1})
		if packErr != nil {
			return packErr
		}
		return tx.PutCell(t.Context(), hexxladb.CellRecord{Key: coord, RawContent: "crash-changefeed"})
	}); err != nil {
		t.Fatalf("update: %v", err)
	}
	t.Fatal("update completed without reaching crash hook (expected SIGKILL while blocked)")
}

// TestIntegration_changefeedCommitPhasesAfterSigKill proves the root transaction boundary,
// durable outbox, and external projection converge after process death. A crash after append may
// redeliver the same mutation, which is the documented at-least-once behavior.
func TestIntegration_changefeedCommitPhasesAfterSigKill(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix-only")
	}
	for _, phase := range []string{"db_header_staged", "db_engine_committed", "db_changelog_appended"} {
		t.Run(phase, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "changefeed-crash.db")
			ready := filepath.Join(dir, "ready")
			if err := startChangefeedChildAndKill9(t, path, ready, phase); err != nil {
				t.Fatal(err)
			}
			opts := &hexxladb.Options{EnableMVCC: true, ChangelogEnabled: true}
			db, err := hexxladb.Open(path, opts)
			if err != nil {
				t.Fatalf("reopen: %v", err)
			}
			defer func() { _ = db.Close() }()
			coord, err := hexxladb.Pack(hexxladb.Coord{Q: 4, R: -1})
			if err != nil {
				t.Fatal(err)
			}
			committed := false
			if err := db.View(func(tx *hexxladb.Tx) error {
				cell, ok, viewErr := tx.GetCell(coord)
				if viewErr != nil {
					return viewErr
				}
				committed = ok
				if ok && cell.RawContent != "crash-changefeed" {
					t.Fatalf("committed cell content: %q", cell.RawContent)
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			changes, err := db.ReadChangelogSince(0, 10)
			if err != nil {
				t.Fatal(err)
			}
			if phase == "db_header_staged" {
				if committed || len(changes) != 0 {
					t.Fatalf("precommit crash became visible: committed=%v changes=%#v", committed, changes)
				}
				return
			}
			if !committed || len(changes) == 0 {
				t.Fatalf("durable commit did not converge: committed=%v changes=%#v", committed, changes)
			}
			for i := range changes {
				if changes[i].Op != hexxladb.ChangelogOpPutCell {
					t.Fatalf("unexpected recovered operation: %#v", changes)
				}
			}
		})
	}
}

// TestIntegration_groupCommitPhasesAfterSigKill spawns a child, waits until it reports reaching a
// named [internal/engine] barrier, then SIGKILL. Reopen must be non-corrupt; visible key, if any,
// must be the full committed value.
func TestIntegration_groupCommitPhasesAfterSigKill(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix-only")
	}
	phases := []string{
		"group_wal_appended",
		"group_wal_synced",
		"group_primary_written",
		"group_primary_synced",
		"group_header_written",
	}
	// Do not use t.Parallel here: each case execs a subprocess that blocks in crashtest.At until
	// SIGKILL. Running them concurrently (default go test -parallel) under -race overwhelms the
	// machine and looks like a hang after repeated "=== RUN   TestIntegration_crashChild" lines.
	for _, phase := range phases {
		t.Run(phase, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "crash.db")
			ready := filepath.Join(dir, "ready")
			if err := startChildAndKill9(t, path, ready, phase); err != nil {
				t.Fatal(err)
			}
			db, err := hexxladb.Open(path, nil)
			if err != nil {
				t.Fatalf("reopen: %v", err)
			}
			defer func() { _ = db.Close() }()
			if err := db.View(func(tx *hexxladb.Tx) error {
				v, ok, e := tx.Get([]byte("crash_key"))
				if e != nil {
					return e
				}
				if ok && string(v) != "crash_val" {
					t.Errorf("torn or corrupt value: %q", v)
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func startChildAndKill9(t *testing.T, dbPath, readyFile, phase string) error {
	t.Helper()
	if os.Getenv("HEXXLADB_IN_CHILD") == "1" {
		return errors.New("unexpected child env in parent")
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestIntegration_crashChild$", "-test.v")
	cmd.Env = append(os.Environ(),
		"HEXXLADB_IN_CHILD=1",
		"HEXXLADB_TEST_DB_PATH="+dbPath,
		"HEXXLADB_TEST_CRASH_AT="+phase,
		"HEXXLADB_TEST_CRASH_READY="+readyFile,
	)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	defer func() { _ = cmd.Wait() }()
	if err := waitFileExists(readyFile, 8*time.Second); err != nil {
		_ = cmd.Process.Kill()
		return err
	}
	return cmd.Process.Kill()
}

func startChangefeedChildAndKill9(t *testing.T, dbPath, readyFile, phase string) error {
	t.Helper()
	if os.Getenv("HEXXLADB_IN_CHILD") == "1" {
		return errors.New("unexpected child env in parent")
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestIntegration_crashChild$", "-test.v")
	cmd.Env = append(os.Environ(),
		"HEXXLADB_IN_CHILD=1",
		"HEXXLADB_TEST_CHANGEFEED=1",
		"HEXXLADB_TEST_DB_PATH="+dbPath,
		"HEXXLADB_TEST_CRASH_AT="+phase,
		"HEXXLADB_TEST_CRASH_READY="+readyFile,
	)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	defer func() { _ = cmd.Wait() }()
	if err := waitFileExists(readyFile, 8*time.Second); err != nil {
		_ = cmd.Process.Kill()
		return err
	}
	return cmd.Process.Kill()
}

func waitFileExists(path string, d time.Duration) error {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	return errors.New("timeout waiting for child ready file")
}
