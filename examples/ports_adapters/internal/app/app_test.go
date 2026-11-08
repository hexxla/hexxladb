package app

import (
	"context"
	"errors"
	"testing"

	"github.com/hexxla/hexxladb/examples/ports_adapters/internal/domain"
)

type recordingStore struct {
	reason, id, note string
}

func (s *recordingStore) RecordConflict(_ context.Context, _, _ domain.Observation, reason string) (string, error) {
	s.reason = reason
	return "recorded", nil
}

func (s *recordingStore) ResolveConflict(_ context.Context, id, note string) error {
	s.id, s.note = id, note
	return nil
}

func TestContradictionService(t *testing.T) {
	t.Parallel()
	store := &recordingStore{}
	service := New(store)
	left := domain.Observation{Text: "The valve is open."}
	right := domain.Observation{At: domain.Position{Q: 1}, Text: "The valve is closed."}
	if _, err := service.Record(t.Context(), left, left, "disagreement"); !errors.Is(err, domain.ErrInvalidInput) || store.reason != "" {
		t.Fatalf("overlapping observations: err=%v, store reason=%q", err, store.reason)
	}
	id, err := service.Record(t.Context(), left, right, " disagreement ")
	if err != nil || id != "recorded" || store.reason != "disagreement" {
		t.Fatalf("record: id=%q, reason=%q, err=%v", id, store.reason, err)
	}
	if err := service.Resolve(t.Context(), id, " "); !errors.Is(err, domain.ErrInvalidInput) || store.id != "" {
		t.Fatalf("empty resolution: err=%v, store ID=%q", err, store.id)
	}
	if err := service.Resolve(t.Context(), id, " Checked in person. "); err != nil || store.id != id || store.note != "Checked in person." {
		t.Fatalf("resolve: id=%q, note=%q, err=%v", store.id, store.note, err)
	}
}
