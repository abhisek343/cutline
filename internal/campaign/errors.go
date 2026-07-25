package campaign

import (
	"errors"
	"fmt"
)

var (
	ErrInvalidCampaign    = errors.New("invalid campaign")
	ErrUnsupportedVersion = errors.New("unsupported campaign version")
)

// FieldError reports a stable field path and preserves its error category.
type FieldError struct {
	Category error
	Field    string
	Reason   string
}

func (e *FieldError) Error() string {
	return fmt.Sprintf("%v: %s: %s", e.Category, e.Field, e.Reason)
}

func (e *FieldError) Unwrap() error {
	return e.Category
}

func fieldError(category error, field, reason string) error {
	return &FieldError{Category: category, Field: field, Reason: reason}
}
