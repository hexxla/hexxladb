// Package crashtest is used by the engine for opt-in integration tests that send SIGKILL to a
// child after a named barrier. Set HEXXLADB_TEST_CRASH_AT to a phase and HEXXLADB_TEST_CRASH_READY
// to a file path; the file is written, then the process blocks until killed.
package crashtest

import (
	"os"
	"time"
)

// At is a no-op unless HEXXLADB_TEST_CRASH_AT matches phase. The ready file, if set, is written
// first; the goroutine then blocks so a parent can kill(2) the process to simulate power loss.
func At(phase string) {
	want := os.Getenv("HEXXLADB_TEST_CRASH_AT")
	if want == "" || want != phase {
		return
	}
	if p := os.Getenv("HEXXLADB_TEST_CRASH_READY"); p != "" {
		//nolint:gosec // G703 test subprocess only; path is chosen by the parent [testing.T] (tempdir).
		_ = os.WriteFile(p, []byte(phase), 0o600)
	}
	for {
		time.Sleep(time.Hour)
	}
}
