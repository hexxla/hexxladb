package hexxladb

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
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
