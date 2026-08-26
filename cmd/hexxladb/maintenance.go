package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/signal"

	"github.com/hexxla/hexxladb"
)

func maintenanceContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt)
}

func optionsFromCredentialEnvironment(passphraseName, keyName string) (*hexxladb.Options, error) {
	passphrase, passphrasePresent := os.LookupEnv(passphraseName)
	encodedKey, keyPresent := os.LookupEnv(keyName)
	if passphrasePresent && keyPresent {
		return nil, fmt.Errorf("%q and %q are both set; choose one credential source", passphraseName, keyName)
	}
	if !passphrasePresent && !keyPresent {
		return nil, nil
	}
	if passphrasePresent {
		if passphrase == "" {
			return nil, fmt.Errorf("environment variable %q is set but empty", passphraseName)
		}
		return &hexxladb.Options{Passphrase: passphrase}, nil
	}
	if encodedKey == "" {
		return nil, fmt.Errorf("environment variable %q is set but empty", keyName)
	}
	key, err := base64.StdEncoding.DecodeString(encodedKey)
	if err != nil {
		return nil, fmt.Errorf("environment variable %q is not standard base64: %w", keyName, err)
	}
	return &hexxladb.Options{EncryptionKey: key}, nil
}

func clearCredentialOptions(opts *hexxladb.Options) {
	if opts == nil {
		return
	}
	clear(opts.EncryptionKey)
	opts.EncryptionKey = nil
	opts.Passphrase = ""
}

func printMaintenanceSpace(space []hexxladb.MaintenanceSpaceRequirement) {
	for _, requirement := range space {
		if requirement.AvailableKnown {
			fmt.Printf(
				"Capacity: %s needs %s; %s available in %q\n",
				requirement.Purpose,
				humanBytesUint(requirement.RequiredBytes),
				humanBytesUint(requirement.AvailableBytes),
				requirement.Directory,
			)
			continue
		}
		fmt.Printf(
			"Capacity: %s needs %s; available space is unavailable on this platform (%q)\n",
			requirement.Purpose,
			humanBytesUint(requirement.RequiredBytes),
			requirement.Directory,
		)
	}
}

func verifyMaintenanceDestination(
	ctx context.Context,
	path string,
	opts *hexxladb.Options,
) (hexxladb.StorageStats, error) {
	db, err := hexxladb.Open(path, opts)
	if err != nil {
		return hexxladb.StorageStats{}, fmt.Errorf("reopen destination: %w", err)
	}
	report, healthErr := db.HealthCheck(ctx, hexxladb.DefaultHealthCheckConfig())
	storage, storageErr := db.StorageStats()
	closeErr := db.Close()
	if healthErr != nil {
		return hexxladb.StorageStats{}, fmt.Errorf("health check destination: %w", healthErr)
	}
	if len(report.OrphanedSeams) > 0 || report.TagIndexErrors > 0 || report.SourceIndexErrors > 0 {
		return hexxladb.StorageStats{}, fmt.Errorf(
			"destination health failed: orphaned_seams=%d tag_index_errors=%d source_index_errors=%d",
			len(report.OrphanedSeams),
			report.TagIndexErrors,
			report.SourceIndexErrors,
		)
	}
	if storageErr != nil {
		return hexxladb.StorageStats{}, fmt.Errorf("destination storage stats: %w", storageErr)
	}
	if closeErr != nil {
		return hexxladb.StorageStats{}, fmt.Errorf("close destination: %w", closeErr)
	}
	return storage, nil
}
