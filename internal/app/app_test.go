package app_test

import (
	"testing"

	"github.com/hexxla/hexxladb/internal/app"
)

func TestNew(t *testing.T) {
	t.Parallel()
	if app.New() == nil {
		t.Fatal("New() returned nil")
	}
}
