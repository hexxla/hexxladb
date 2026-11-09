package main

import (
	"bytes"
	"encoding/base64"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hexxla/hexxladb"
	"github.com/hexxla/hexxladb/internal/cmdconfig"
)

func TestCmdCompactDryRunThenVerifiedCopy(t *testing.T) {
	directory := t.TempDir()
	sourcePath := filepath.Join(directory, "source.db")
	destinationPath := filepath.Join(directory, "destination.db")
	seedMaintenanceCommandDB(t, sourcePath, &hexxladb.Options{})

	code, stdout, stderr := captureMaintenanceCommand(t, func() int {
		return cmdCompact([]string{"--dry-run", "-o", destinationPath, sourcePath})
	})
	if code != 0 {
		t.Fatalf("dry-run code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "Compaction preflight OK") {
		t.Fatalf("dry-run stdout=%q", stdout)
	}
	if _, err := os.Stat(destinationPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("dry-run created destination: %v", err)
	}

	code, stdout, stderr = captureMaintenanceCommand(t, func() int {
		return cmdCompact([]string{"--batch-size", "1", "-o", destinationPath, sourcePath})
	})
	if code != 0 {
		t.Fatalf("compact code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "health=ok") || !strings.Contains(stdout, "Source preserved") {
		t.Fatalf("compact stdout=%q", stdout)
	}
	source, err := hexxladb.Open(sourcePath, &hexxladb.Options{})
	if err != nil {
		t.Fatalf("source no longer openable: %v", err)
	}
	if err := source.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestOptionsFromCredentialEnvironment(t *testing.T) {
	const (
		passphraseName = "HEXXLADB_TEST_PASSPHRASE"
		keyName        = "HEXXLADB_TEST_KEY"
	)
	t.Setenv(passphraseName, "secret-passphrase")
	opts, err := cmdconfig.CredentialOptions(passphraseName, keyName)
	if err != nil || opts == nil || opts.Passphrase != "secret-passphrase" {
		t.Fatalf("passphrase options=%#v err=%v", opts, err)
	}
	cmdconfig.ClearCredentials(opts)
	if opts.Passphrase != "" {
		t.Fatal("passphrase was not cleared")
	}

	t.Setenv(passphraseName, "")
	wantKey := bytes.Repeat([]byte{0x5a}, 32)
	t.Setenv(keyName, base64.StdEncoding.EncodeToString(wantKey))
	opts, err = cmdconfig.CredentialOptions("HEXXLADB_TEST_UNSET", keyName)
	if err != nil || opts == nil || !bytes.Equal(opts.EncryptionKey, wantKey) {
		t.Fatalf("key options=%#v err=%v", opts, err)
	}
	cmdconfig.ClearCredentials(opts)
	if opts.EncryptionKey != nil {
		t.Fatal("encryption key was not cleared")
	}

	t.Setenv(passphraseName, "do-not-print-passphrase")
	_, err = cmdconfig.CredentialOptions(passphraseName, keyName)
	if err == nil || strings.Contains(err.Error(), "do-not-print-passphrase") || strings.Contains(err.Error(), base64.StdEncoding.EncodeToString(wantKey)) {
		t.Fatalf("mutual credential error=%q", err)
	}
}

func TestMaintenanceCommandHelpExitsSuccessfully(t *testing.T) {
	for _, command := range []struct {
		name string
		run  func([]string) int
	}{
		{name: "compact", run: cmdCompact},
	} {
		t.Run(command.name, func(t *testing.T) {
			code, _, _ := captureMaintenanceCommand(t, func() int {
				return command.run([]string{"-h"})
			})
			if code != 0 {
				t.Fatalf("help exit code=%d, want 0", code)
			}
		})
	}
}

func seedMaintenanceCommandDB(t *testing.T, path string, opts *hexxladb.Options) {
	t.Helper()
	db, err := hexxladb.Open(path, opts)
	if err != nil {
		t.Fatal(err)
	}
	key, err := hexxladb.Pack(hexxladb.Coord{Q: 1, R: -1})
	if err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if err := db.Update(func(tx *hexxladb.Tx) error {
		return tx.PutCell(t.Context(), hexxladb.CellRecord{Key: key, RawContent: "maintenance fixture"})
	}); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
}

func captureMaintenanceCommand(t *testing.T, command func() int) (code int, stdout, stderr string) {
	t.Helper()
	originalStdout := os.Stdout
	originalStderr := os.Stderr
	stdoutReader, stdoutWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stderrReader, stderrWriter, err := os.Pipe()
	if err != nil {
		_ = stdoutReader.Close()
		_ = stdoutWriter.Close()
		t.Fatal(err)
	}
	os.Stdout = stdoutWriter
	os.Stderr = stderrWriter
	defer func() {
		os.Stdout = originalStdout
		os.Stderr = originalStderr
	}()

	code = command()
	if err := stdoutWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := stderrWriter.Close(); err != nil {
		t.Fatal(err)
	}
	stdoutBytes, err := io.ReadAll(stdoutReader)
	if err != nil {
		t.Fatal(err)
	}
	stderrBytes, err := io.ReadAll(stderrReader)
	if err != nil {
		t.Fatal(err)
	}
	if err := stdoutReader.Close(); err != nil {
		t.Fatal(err)
	}
	if err := stderrReader.Close(); err != nil {
		t.Fatal(err)
	}
	return code, string(stdoutBytes), string(stderrBytes)
}
