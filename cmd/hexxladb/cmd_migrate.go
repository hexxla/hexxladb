package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/hexxla/hexxladb"
)

func cmdMigrateV1ToV2(args []string) int {
	return cmdMigrate(args, false)
}

func cmdMigrateToAuthenticated(args []string) int {
	return cmdMigrate(args, true)
}

func cmdMigrate(args []string, authenticated bool) int {
	command := "migrate-v1-to-v2"
	destinationDescription := "Destination format-v2 database file (required)"
	summary := "Offline, resumable, source-preserving format-v1 to format-v2 migration."
	if authenticated {
		command = "migrate-to-authenticated"
		destinationDescription = "Destination authenticated format-v3 database file (required)"
		summary = "Offline, source-preserving format-v1/v2 to authenticated format-v3 migration."
	}
	fs := flag.NewFlagSet(command, flag.ContinueOnError)
	output := fs.String("o", "", destinationDescription)
	dryRun := fs.Bool("dry-run", false, "Run exact checks without creating a destination or copying a new migration batch")
	batchSize := fs.Int("batch-size", 0, "Source keys per durable batch (default 4096; maximum 4096)")
	resetChangelog := fs.Bool("reset-changelog", false, "Explicitly start a new logical changefeed history")
	snapshotDirectory := fs.String("snapshot-dir", "", "Existing directory for the temporary locked source snapshot (default destination directory)")
	sourcePassphraseEnvironment := fs.String(
		"source-passphrase-env",
		"HEXXLA_SOURCE_PASSPHRASE",
		"Environment variable containing the source passphrase",
	)
	sourceKeyEnvironment := fs.String(
		"source-encryption-key-env",
		"HEXXLA_SOURCE_ENCRYPTION_KEY",
		"Environment variable containing the source encryption key as standard base64",
	)
	destinationPassphraseEnvironment := fs.String(
		"destination-passphrase-env",
		"HEXXLA_DESTINATION_PASSPHRASE",
		"Environment variable containing the destination passphrase",
	)
	destinationKeyEnvironment := fs.String(
		"destination-encryption-key-env",
		"HEXXLA_DESTINATION_ENCRYPTION_KEY",
		"Environment variable containing the destination encryption key as standard base64",
	)
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: hexxladb %s [flags] -o <dest> <src>\n", command)
		fmt.Fprintf(os.Stderr, "\n%s\n", summary)
		fmt.Fprintln(os.Stderr, "Credentials are read only from named environment variables, never command arguments.")
		fmt.Fprintln(os.Stderr)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() != 1 || *output == "" {
		fs.Usage()
		return 2
	}

	sourceOptions, err := optionsFromCredentialEnvironment(*sourcePassphraseEnvironment, *sourceKeyEnvironment)
	if err != nil {
		fmt.Fprintf(os.Stderr, "hexxladb %s: source credentials: %v\n", command, err)
		return 1
	}
	defer clearCredentialOptions(sourceOptions)
	destinationOptions, err := optionsFromCredentialEnvironment(*destinationPassphraseEnvironment, *destinationKeyEnvironment)
	if err != nil {
		fmt.Fprintf(os.Stderr, "hexxladb %s: destination credentials: %v\n", command, err)
		return 1
	}
	defer clearCredentialOptions(destinationOptions)

	srcPath := fs.Arg(0)
	destPath := *output
	options := &hexxladb.MigrationOptions{
		SourceOptions:      sourceOptions,
		DestinationOptions: destinationOptions,
		BatchSize:          *batchSize,
		SnapshotDirectory:  *snapshotDirectory,
		ResetChangelog:     *resetChangelog,
	}
	ctx, cancel := maintenanceContext()
	defer cancel()

	if *dryRun {
		var preflight hexxladb.MigrationPreflight
		if authenticated {
			preflight, err = hexxladb.PreflightMigrateToAuthenticated(ctx, srcPath, destPath, options)
		} else {
			preflight, err = hexxladb.PreflightMigrateV1ToV2(ctx, srcPath, destPath, options)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "hexxladb %s: %v\n", command, err)
			return 1
		}
		printMigrationPreflight(preflight)
		fmt.Println("Migration preflight OK; no destination was created and no new migration batch was copied.")
		return 0
	}

	options.OnPreflight = printMigrationPreflight
	options.OnProgress = func(progress hexxladb.MigrationProgress) {
		state := "copied"
		if progress.Resumed {
			state = "resumed"
		}
		fmt.Fprintf(
			os.Stderr,
			"hexxladb %s: %s at %d source keys (durable)\n",
			command,
			state,
			progress.ProcessedKeys,
		)
	}
	if authenticated {
		err = hexxladb.MigrateToAuthenticated(ctx, srcPath, destPath, options)
	} else {
		err = hexxladb.MigrateV1ToV2(ctx, srcPath, destPath, options)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "hexxladb %s: %v\n", command, err)
		return 1
	}

	destinationStorage, err := verifyMaintenanceDestination(ctx, destPath, destinationOptions)
	if err != nil {
		fmt.Fprintf(os.Stderr, "hexxladb %s: verify: %v\n", command, err)
		return 1
	}
	fmt.Printf(
		"Migration candidate verified: %q (primary=%s, live=%s, health=ok)\n",
		destPath,
		humanBytesUint(destinationStorage.PrimaryBytes),
		humanBytesUint(destinationStorage.LiveBytes),
	)
	fmt.Println("Source preserved. Back up and validate the candidate with application probes before any explicit replacement; retain the complete source recovery set for rollback.")
	return 0
}

func printMigrationPreflight(preflight hexxladb.MigrationPreflight) {
	fmt.Printf(
		"Source: primary=%s wal=%s live=%s reclaimable=%s\n",
		humanBytesUint(preflight.SourcePrimaryBytes),
		humanBytesUint(preflight.SourceWALBytes),
		humanBytesUint(preflight.SourceStorage.LiveBytes),
		humanBytesUint(preflight.SourceStorage.ReclaimableBytes),
	)
	if preflight.Resumable {
		fmt.Printf("Destination: matching resumable migration at %d processed keys\n", preflight.ProcessedKeys)
	} else {
		fmt.Println("Destination: absent and available for exclusive creation")
	}
	printMaintenanceSpace(preflight.Space)
}
