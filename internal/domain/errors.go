package domain

import "errors"

// ErrInvalidInput is returned when input fails validation (e.g. empty after trim).
var ErrInvalidInput = errors.New("invalid input")

// ErrContentTooLarge is returned when input exceeds [MaxContentLen] bytes.
var ErrContentTooLarge = errors.New("content exceeds maximum length")
