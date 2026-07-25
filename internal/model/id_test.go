package model

import (
	"errors"
	"strings"
	"testing"
)

func TestNewID(t *testing.T) {
	t.Parallel()

	first, err := NewID("run")
	if err != nil {
		t.Fatalf("NewID() error = %v", err)
	}
	second, err := NewID("run")
	if err != nil {
		t.Fatalf("NewID() second error = %v", err)
	}
	if first == second {
		t.Fatal("NewID() returned duplicate values")
	}
	if err := ValidateID(first); err != nil {
		t.Fatalf("ValidateID(%q) error = %v", first, err)
	}
}

func TestContentIDIsStableAndUnambiguous(t *testing.T) {
	t.Parallel()

	first, err := ContentID("point", "ab", "c")
	if err != nil {
		t.Fatalf("ContentID() error = %v", err)
	}
	second, err := ContentID("point", "ab", "c")
	if err != nil {
		t.Fatalf("ContentID() second error = %v", err)
	}
	ambiguous, err := ContentID("point", "a", "bc")
	if err != nil {
		t.Fatalf("ContentID() ambiguous error = %v", err)
	}
	if first != second {
		t.Fatalf("ContentID() not stable: %q != %q", first, second)
	}
	if first == ambiguous {
		t.Fatal("ContentID() did not length-delimit parts")
	}
}

func TestInvalidIDs(t *testing.T) {
	t.Parallel()

	tests := []string{
		"",
		"RUN_0123456789abcdef0123456789abcdef",
		"run",
		"run_short",
		"run_0123456789abcdef0123456789abcdeg",
		strings.Repeat("a", 25) + "_0123456789abcdef0123456789abcdef",
	}
	for _, value := range tests {
		if err := ValidateID(value); !errors.Is(err, ErrInvalidID) {
			t.Errorf("ValidateID(%q) error = %v, want ErrInvalidID", value, err)
		}
	}
	if _, err := NewID("Bad"); !errors.Is(err, ErrInvalidID) {
		t.Fatalf("NewID(Bad) error = %v, want ErrInvalidID", err)
	}
}
