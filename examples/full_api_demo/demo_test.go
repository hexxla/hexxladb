package main

import (
	"context"
	"testing"
)

func TestFullAPIDemoSmoke(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := runTour(context.Background(), dir, false, true); err != nil {
		t.Fatal(err)
	}
}
