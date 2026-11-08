package domain

import (
	"context"
	"errors"
)

var (
	// ErrInvalidInput indicates a request that fails application validation.
	ErrInvalidInput = errors.New("invalid contradiction input")
	// ErrConflictNotFound indicates an unknown contradiction ID.
	ErrConflictNotFound = errors.New("contradiction not found")
)

// Position locates an observation on the application's hex grid.
type Position struct {
	Q, R int
}

// Observation is one side of a contradiction.
type Observation struct {
	At   Position
	Text string
}

// ConflictStore persists the application's contradiction workflow.
type ConflictStore interface {
	// RecordConflict stores both observations and their link in one operation.
	RecordConflict(ctx context.Context, left, right Observation, reason string) (id string, err error)
	// ResolveConflict records the operator's resolution note on a saved conflict.
	ResolveConflict(ctx context.Context, id, note string) error
}
