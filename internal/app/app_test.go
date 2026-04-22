package app_test

import (
	"context"
	"errors"
	"testing"

	"github.com/hexxla/hexxladb/internal/app"
	"github.com/hexxla/hexxladb/internal/record"
)

func TestNew(t *testing.T) {
	t.Parallel()
	if app.New() == nil {
		t.Fatal("New() returned nil")
	}
}

func TestService_PutCell_ErrNoStorage(t *testing.T) {
	t.Parallel()
	s := app.New()
	err := s.PutCell(context.Background(), record.CellRecord{})
	if !errors.Is(err, app.ErrNoStorage) {
		t.Fatalf("PutCell: %v", err)
	}
}
