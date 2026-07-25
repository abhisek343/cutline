package model

import (
	"errors"
	"fmt"
)

var (
	ErrInvalidID         = errors.New("invalid id")
	ErrInvalidEvent      = errors.New("invalid event")
	ErrInvalidTransition = errors.New("invalid state transition")
)

// ValidationError identifies one invalid field without discarding its category.
type ValidationError struct {
	Category error
	Field    string
	Reason   string
}

func (e *ValidationError) Error() string {
	if e.Field == "" {
		return fmt.Sprintf("%v: %s", e.Category, e.Reason)
	}
	return fmt.Sprintf("%v: %s: %s", e.Category, e.Field, e.Reason)
}

func (e *ValidationError) Unwrap() error {
	return e.Category
}

func invalid(category error, field, reason string) error {
	return &ValidationError{Category: category, Field: field, Reason: reason}
}
