package hexxladb

import (
	"errors"
	"math"
	"testing"
)

func TestInspectMaintenanceSpaceRejectsInsufficientCapacity(t *testing.T) {
	requirements, err := inspectMaintenanceSpace([]maintenanceSpacePart{{
		directory: t.TempDir(),
		purpose:   "test destination",
		required:  math.MaxUint64,
	}})
	if !errors.Is(err, ErrInsufficientSpace) {
		t.Fatalf("error=%v, want ErrInsufficientSpace", err)
	}
	if len(requirements) != 1 || !requirements[0].AvailableKnown {
		t.Fatalf("requirements=%#v", requirements)
	}
}

func TestInspectMaintenanceSpaceCombinesSameFilesystem(t *testing.T) {
	directory := t.TempDir()
	requirements, err := inspectMaintenanceSpace([]maintenanceSpacePart{
		{directory: directory, purpose: "snapshot", required: 100},
		{directory: directory, purpose: "destination", required: 200},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(requirements) != 1 || requirements[0].RequiredBytes != 300 {
		t.Fatalf("requirements=%#v", requirements)
	}
}
