//go:build linux

package hexxladb

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRecoverInterruptedRotationRejectsSymlinkBackup(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	primary := filepath.Join(directory, "database.db")
	target := filepath.Join(directory, "target")
	writeTestFile(t, primary, "new-primary")
	writeTestFile(t, target, "unrelated")
	if err := os.Symlink(target, primary+".rotate.bak"); err != nil {
		t.Fatal(err)
	}
	if err := writeRotationState(primary, rotationState{Version: rotationStateVersion}); err != nil {
		t.Fatal(err)
	}

	err := RecoverInterruptedRotation(primary, nil)
	if !errors.Is(err, ErrRotationIncomplete) {
		t.Fatalf("recovery error = %v, want ErrRotationIncomplete", err)
	}
	assertTestFileContent(t, primary, "new-primary")
	assertTestFileContent(t, target, "unrelated")
}
