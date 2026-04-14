package record_test

import (
	"testing"

	"github.com/hexxla/hexxladb/internal/record"
)

func TestValidAt(t *testing.T) {
	t.Parallel()
	var (
		lo int64 = 100
		hi int64 = 200
	)
	tests := []struct {
		name string
		v    record.ValidityWire
		t    int64
		want bool
	}{
		{"both_nil_always", record.ValidityWire{}, 0, true},
		{"both_nil_large_t", record.ValidityWire{}, 1 << 62, true},
		{"inside_half_open", record.ValidityWire{ValidFrom: &lo, ValidTo: &hi}, 150, true},
		{"at_lower_inclusive", record.ValidityWire{ValidFrom: &lo, ValidTo: &hi}, 100, true},
		{"at_upper_exclusive", record.ValidityWire{ValidFrom: &lo, ValidTo: &hi}, 200, false},
		{"below_window", record.ValidityWire{ValidFrom: &lo, ValidTo: &hi}, 99, false},
		{"above_window", record.ValidityWire{ValidFrom: &lo, ValidTo: &hi}, 200, false},
		{"open_upper", record.ValidityWire{ValidFrom: &lo, ValidTo: nil}, 1 << 40, true},
		{"open_lower", record.ValidityWire{ValidFrom: nil, ValidTo: &hi}, 50, true},
		{"open_lower_above", record.ValidityWire{ValidFrom: nil, ValidTo: &hi}, 200, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := record.ValidAt(tc.v, tc.t); got != tc.want {
				t.Fatalf("ValidAt(%+v, %d) = %v, want %v", tc.v, tc.t, got, tc.want)
			}
		})
	}
}
