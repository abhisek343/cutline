package model

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
)

const randomIDBytes = 16

var prefixPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,23}$`)

type (
	RunID          string
	AttemptID      string
	SessionID      string
	TaskID         string
	CheckpointID   string
	VisitID        string
	CancellationID string
	EffectID       string
	ResourceID     string
)

// NewID creates an opaque random identifier with a validated semantic prefix.
func NewID(prefix string) (string, error) {
	if !prefixPattern.MatchString(prefix) {
		return "", invalid(ErrInvalidID, "prefix", "must match [a-z][a-z0-9-]{0,23}")
	}
	raw := make([]byte, randomIDBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate %s id: %w", prefix, err)
	}
	return prefix + "_" + hex.EncodeToString(raw), nil
}

// ContentID creates a stable identifier from a prefix and unambiguous parts.
func ContentID(prefix string, parts ...string) (string, error) {
	if !prefixPattern.MatchString(prefix) {
		return "", invalid(ErrInvalidID, "prefix", "must match [a-z][a-z0-9-]{0,23}")
	}
	h := sha256.New()
	for _, part := range parts {
		fmt.Fprintf(h, "%d:", len(part))
		h.Write([]byte(part))
	}
	return prefix + "_" + hex.EncodeToString(h.Sum(nil)[:randomIDBytes]), nil
}

// ValidateID checks the common opaque-ID representation.
func ValidateID(value string) error {
	prefix, encoded, ok := strings.Cut(value, "_")
	if !ok || !prefixPattern.MatchString(prefix) {
		return invalid(ErrInvalidID, "", "must contain a valid prefix and underscore")
	}
	if len(encoded) != randomIDBytes*2 {
		return invalid(ErrInvalidID, "", "random component must contain 32 hexadecimal characters")
	}
	if _, err := hex.DecodeString(encoded); err != nil {
		return invalid(ErrInvalidID, "", "random component must be hexadecimal")
	}
	return nil
}
