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
)

func TestCmdMigrateV1ToV2DryRunThenVerifiedCopy(t *testing.T) {
	directory := t.TempDir()
	sourcePath := filepath.Join(directory, "source.db")
	destinationPath := filepath.Join(directory, "destination.db")
	seedMaintenanceCommandDB(t, sourcePath, nil)

	code, stdout, stderr := captureMaintenanceCommand(t, func() int {
		return cmdMigrateV1ToV2([]string{"--dry-run", "-o", destinationPath, sourcePath})
	})
	if code != 0 {
		t.Fatalf("dry-run code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "Migration preflight OK") {
		t.Fatalf("dry-run stdout=%q", stdout)
	}
	if _, err := os.Stat(destinationPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("dry-run created destination: %v", err)
	}

	code, stdout, stderr = captureMaintenanceCommand(t, func() int {
		return cmdMigrateV1ToV2([]string{"--batch-size", "1", "-o", destinationPath, sourcePath})
	})
	if code != 0 {
		t.Fatalf("migrate code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "Migration candidate verified") || !strings.Contains(stdout, "Source preserved") {
		t.Fatalf("migrate stdout=%q", stdout)
	}
	destination, err := hexxladb.Open(destinationPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	stats, err := destination.StatsMVCC()
	closeErr := destination.Close()
	if err != nil || closeErr != nil || stats.CommitSeq == 0 || stats.VersionedRows == 0 {
		t.Fatalf("destination stats=%#v statsErr=%v closeErr=%v", stats, err, closeErr)
	}
	source, err := hexxladb.Open(sourcePath, nil)
	if err != nil {
		t.Fatalf("source no longer openable: %v", err)
	}
	if err := source.Close(); err != nil {
		t.Fatal(err)
	}
	matches, err := filepath.Glob(filepath.Join(directory, ".hexxladb-migrate-v1-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("migration left snapshots: %v", matches)
	}
}

func TestCmdMigrateToAuthenticatedDryRunThenVerifiedCopy(t *testing.T) {
	directory := t.TempDir()
	sourcePath := filepath.Join(directory, "source.db")
	destinationPath := filepath.Join(directory, "authenticated.db")
	seedMaintenanceCommandDB(t, sourcePath, &hexxladb.Options{EnableMVCC: true})
	const passphraseEnvironment = "HEXXLA_TEST_AUTHENTICATED_DESTINATION"
	t.Setenv(passphraseEnvironment, "authenticated command passphrase")

	args := []string{
		"--destination-passphrase-env", passphraseEnvironment,
		"--destination-encryption-key-env", "HEXXLA_TEST_UNSET_AUTHENTICATED_KEY",
		"-o", destinationPath,
		sourcePath,
	}
	code, stdout, stderr := captureMaintenanceCommand(t, func() int {
		return cmdMigrateToAuthenticated(append([]string{"--dry-run"}, args...))
	})
	if code != 0 || !strings.Contains(stdout, "Migration preflight OK") {
		t.Fatalf("dry-run code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if _, err := os.Stat(destinationPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("dry-run created destination: %v", err)
	}

	code, stdout, stderr = captureMaintenanceCommand(t, func() int {
		return cmdMigrateToAuthenticated(args)
	})
	if code != 0 || !strings.Contains(stdout, "Migration candidate verified") {
		t.Fatalf("migrate code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	destination, err := hexxladb.Open(destinationPath, &hexxladb.Options{Passphrase: "authenticated command passphrase"})
	if err != nil {
		t.Fatal(err)
	}
	if err := destination.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestCmdCompactDryRunThenVerifiedCopy(t *testing.T) {
	directory := t.TempDir()
	sourcePath := filepath.Join(directory, "source.db")
	destinationPath := filepath.Join(directory, "destination.db")
	seedMaintenanceCommandDB(t, sourcePath, &hexxladb.Options{EnableMVCC: true})

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
	source, err := hexxladb.Open(sourcePath, &hexxladb.Options{EnableMVCC: true})
	if err != nil {
		t.Fatalf("source no longer openable: %v", err)
	}
	if err := source.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestCmdMigrateEncryptionKeysAreEnvironmentOnlyAndNotPrinted(t *testing.T) {
	directory := t.TempDir()
	sourcePath := filepath.Join(directory, "source.db")
	destinationPath := filepath.Join(directory, "destination.db")
	sourceKey := bytes.Repeat([]byte{0x31}, 32)
	destinationKey := bytes.Repeat([]byte{0x42}, 32)
	seedMaintenanceCommandDB(t, sourcePath, nil)

	const (
		sourceKeyName      = "HEXXLA_TEST_SOURCE_KEY"
		destinationKeyName = "HEXXLA_TEST_DESTINATION_KEY"
	)
	encodedSource := base64.StdEncoding.EncodeToString(sourceKey)
	encodedDestination := base64.StdEncoding.EncodeToString(destinationKey)
	t.Setenv(sourceKeyName, encodedSource)
	t.Setenv(destinationKeyName, encodedDestination)

	failureCode, failureStdout, failureStderr := captureMaintenanceCommand(t, func() int {
		return cmdMigrateV1ToV2([]string{
			"--dry-run",
			"--source-passphrase-env", "HEXXLA_TEST_UNSET_SOURCE_PASSPHRASE",
			"--source-encryption-key-env", sourceKeyName,
			"-o", destinationPath,
			sourcePath,
		})
	})
	if failureCode == 0 {
		t.Fatal("plaintext source unexpectedly accepted an encryption key")
	}
	if strings.Contains(failureStdout+failureStderr, encodedSource) {
		t.Fatalf("source credential appeared in command output: %q", failureStdout+failureStderr)
	}

	code, stdout, stderr := captureMaintenanceCommand(t, func() int {
		return cmdMigrateV1ToV2([]string{
			"--source-passphrase-env", "HEXXLA_TEST_UNSET_SOURCE_PASSPHRASE",
			"--destination-passphrase-env", "HEXXLA_TEST_UNSET_DESTINATION_PASSPHRASE",
			"--destination-encryption-key-env", destinationKeyName,
			"-o", destinationPath,
			sourcePath,
		})
	})
	if code != 0 {
		t.Fatalf("migrate code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	combined := stdout + stderr
	if strings.Contains(combined, encodedSource) || strings.Contains(combined, encodedDestination) {
		t.Fatalf("credential appeared in command output: %q", combined)
	}
	destination, err := hexxladb.Open(destinationPath, &hexxladb.Options{EncryptionKey: destinationKey})
	if err != nil {
		t.Fatal(err)
	}
	if err := destination.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestOptionsFromCredentialEnvironment(t *testing.T) {
	const (
		passphraseName = "HEXXLADB_TEST_PASSPHRASE"
		keyName        = "HEXXLADB_TEST_KEY"
	)
	t.Setenv(passphraseName, "secret-passphrase")
	opts, err := optionsFromCredentialEnvironment(passphraseName, keyName)
	if err != nil || opts == nil || opts.Passphrase != "secret-passphrase" {
		t.Fatalf("passphrase options=%#v err=%v", opts, err)
	}
	clearCredentialOptions(opts)
	if opts.Passphrase != "" {
		t.Fatal("passphrase was not cleared")
	}

	t.Setenv(passphraseName, "")
	wantKey := bytes.Repeat([]byte{0x5a}, 32)
	t.Setenv(keyName, base64.StdEncoding.EncodeToString(wantKey))
	opts, err = optionsFromCredentialEnvironment("HEXXLADB_TEST_UNSET", keyName)
	if err != nil || opts == nil || !bytes.Equal(opts.EncryptionKey, wantKey) {
		t.Fatalf("key options=%#v err=%v", opts, err)
	}
	clearCredentialOptions(opts)
	if opts.EncryptionKey != nil {
		t.Fatal("encryption key was not cleared")
	}

	t.Setenv(passphraseName, "do-not-print-passphrase")
	_, err = optionsFromCredentialEnvironment(passphraseName, keyName)
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
		{name: "migrate-v1-to-v2", run: cmdMigrateV1ToV2},
		{name: "migrate-to-authenticated", run: cmdMigrateToAuthenticated},
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
