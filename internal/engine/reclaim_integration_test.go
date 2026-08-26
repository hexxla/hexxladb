//go:build integration

package engine

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestIntegrationEngineReclaimChild(t *testing.T) {
	if os.Getenv("HEXXLADB_RECLAIM_CHILD") != "1" {
		t.Skip("reclaim child only")
	}
	path := os.Getenv("HEXXLADB_TEST_DB_PATH")
	e, err := Open(path, authenticatedRecoveryTestOptions())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = e.Close() }()
	if _, err := e.ReclaimTail(); err != nil {
		t.Fatal(err)
	}
	t.Fatal("reclaim completed without reaching crash hook")
}

func TestIntegrationAuthenticatedReclaimPhasesAfterSigKill(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix-only")
	}
	for _, phase := range []string{
		"authenticated_header_only_wal_synced",
		"authenticated_header_only_header_written",
		"reclaim_header_committed",
		"reclaim_primary_truncated",
	} {
		t.Run(phase, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "reclaim.db")
			opts := authenticatedRecoveryTestOptions()
			e, err := Open(path, opts)
			if err != nil {
				t.Fatal(err)
			}
			tree := OpenBTree(e)
			commitTreeMutation(t, e, func() error { return tree.Put([]byte("key"), []byte("value")) })
			commitTreeMutation(t, e, func() error { return tree.Delete([]byte("key")) })
			if err := e.Close(); err != nil {
				t.Fatal(err)
			}

			ready := filepath.Join(dir, "ready")
			if err := startReclaimChildAndKill(t, path, ready, phase); err != nil {
				t.Fatal(err)
			}
			reopened, err := Open(path, opts)
			if err != nil {
				t.Fatalf("reopen: %v", err)
			}
			if _, ok, err := OpenBTree(reopened).Get([]byte("key")); err != nil || ok {
				_ = reopened.Close()
				t.Fatalf("deleted key after recovery: ok=%v err=%v", ok, err)
			}
			if _, err := reopened.ReclaimTail(); err != nil {
				_ = reopened.Close()
				t.Fatalf("retry reclaim: %v", err)
			}
			if err := reopened.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func startReclaimChildAndKill(t *testing.T, dbPath, readyFile, phase string) error {
	t.Helper()
	if os.Getenv("HEXXLADB_RECLAIM_CHILD") == "1" {
		return errors.New("unexpected reclaim child env in parent")
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestIntegrationEngineReclaimChild$", "-test.v")
	cmd.Env = append(os.Environ(),
		"HEXXLADB_RECLAIM_CHILD=1",
		"HEXXLADB_TEST_DB_PATH="+dbPath,
		"HEXXLADB_TEST_CRASH_AT="+phase,
		"HEXXLADB_TEST_CRASH_READY="+readyFile,
	)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	defer func() { _ = cmd.Wait() }()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(readyFile); err == nil {
			return cmd.Process.Kill()
		}
		time.Sleep(20 * time.Millisecond)
	}
	_ = cmd.Process.Kill()
	return errors.New("timeout waiting for reclaim child")
}
