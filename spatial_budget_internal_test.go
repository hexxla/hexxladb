package hexxladb

import (
	"errors"
	"testing"
)

func TestSeamSearchBudgetLimits(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		limit int
		add   func(*seamSearchBudget) error
	}{
		{name: "index rows", limit: MaxSeamIndexRows, add: (*seamSearchBudget).addIndexRow},
		{name: "results", limit: MaxSeamSearchResults, add: (*seamSearchBudget).addResult},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			budget := &seamSearchBudget{}
			for range test.limit {
				if err := test.add(budget); err != nil {
					t.Fatalf("within limit: %v", err)
				}
			}
			if err := test.add(budget); !errors.Is(err, ErrSpatialScanLimit) {
				t.Fatalf("over limit error = %v, want ErrSpatialScanLimit", err)
			}
		})
	}
}
