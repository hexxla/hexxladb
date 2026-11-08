package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/hexxla/hexxladb"
)

func maintenanceContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt)
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
	checkOrphans bool,
) (hexxladb.StorageStats, error) {
	db, err := hexxladb.Open(path, opts)
	if err != nil {
		return hexxladb.StorageStats{}, fmt.Errorf("reopen destination: %w", err)
	}
	healthConfig := hexxladb.DefaultHealthCheckConfig()
	healthConfig.CheckOrphans = checkOrphans
	report, healthErr := db.HealthCheck(ctx, healthConfig)
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
