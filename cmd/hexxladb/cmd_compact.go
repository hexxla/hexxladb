package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/hexxla/hexxladb"
)

func cmdCompact(args []string) int {
	fs := flag.NewFlagSet("compact", flag.ContinueOnError)
	output := fs.String("o", "", "Destination database file (required)")
	dryRun := fs.Bool("dry-run", false, "Run source, path, and capacity checks without creating the destination")
	batchSize := fs.Int("batch-size", 0, "Physical keys per durable batch (default 4096; maximum 4096)")
	passphraseEnvironment := fs.String(
		"passphrase-env",
		"HEXXLA_PASSPHRASE",
		"Environment variable containing the source passphrase (never the passphrase itself)",
	)
	keyEnvironment := fs.String(
		"encryption-key-env",
		"HEXXLA_ENCRYPTION_KEY",
		"Environment variable containing the source encryption key as standard base64",
	)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: hexxladb compact [flags] -o <dest> <src>")
		fmt.Fprintln(os.Stderr, "\nCopy-compact a database to a new file. The source is not modified.")
		fmt.Fprintln(os.Stderr, "All data including MVCC history is preserved. Reclaims freed overflow pages.")
		fmt.Fprintln(os.Stderr, "Encrypted credentials are read only from one named environment variable.")
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
	srcPath := fs.Arg(0)
	dstPath := *output

	sourceOptions, err := optionsFromCredentialEnvironment(*passphraseEnvironment, *keyEnvironment)
	if err != nil {
		fmt.Fprintf(os.Stderr, "hexxladb compact: credentials: %v\n", err)
		return 1
	}
	defer clearCredentialOptions(sourceOptions)
	ctx, cancel := maintenanceContext()
	defer cancel()
	preflight, err := hexxladb.PreflightCompactTo(ctx, srcPath, dstPath, sourceOptions)
	if err != nil {
		fmt.Fprintf(os.Stderr, "hexxladb compact: %v\n", err)
		return 1
	}
	fmt.Printf(
		"Source: primary=%s live=%s reclaimable=%s wal=%s\n",
		humanBytesUint(preflight.SourceStorage.PrimaryBytes),
		humanBytesUint(preflight.SourceStorage.LiveBytes),
		humanBytesUint(preflight.SourceStorage.ReclaimableBytes),
		humanBytesUint(preflight.SourceStorage.WALBytes),
	)
	printMaintenanceSpace(preflight.Space)
	if *dryRun {
		fmt.Println("Compaction preflight OK; no destination was created.")
		return 0
	}

	if err := hexxladb.CompactToWithOptions(ctx, srcPath, dstPath, sourceOptions, &hexxladb.CompactOptions{
		BatchSize:         *batchSize,
		VerifyDestination: true,
		OnProgress: func(progress hexxladb.CompactProgress) {
			fmt.Fprintf(os.Stderr, "hexxladb compact: copied %d keys (durable)\n", progress.CopiedKeys)
		},
	}); err != nil {
		fmt.Fprintf(os.Stderr, "hexxladb compact: %v\n", err)
		return 1
	}

	destinationStorage, err := verifyMaintenanceDestination(ctx, dstPath, sourceOptions)
	if err != nil {
		fmt.Fprintf(os.Stderr, "hexxladb compact: verify: %v\n", err)
		return 1
	}

	gain := float64(preflight.SourceStorage.PrimaryBytes) / float64(max(uint64(1), destinationStorage.PrimaryBytes))
	fmt.Printf("%q → %q  (%s → %s, gain=%.2fx, health=ok)\n",
		srcPath, dstPath,
		humanBytesUint(preflight.SourceStorage.PrimaryBytes),
		humanBytesUint(destinationStorage.PrimaryBytes),
		gain,
	)
	fmt.Println("Source preserved. Keep its primary, WAL, changelog, and backup until the candidate is replaced, reopened, and validated in the application.")
	return 0
}
