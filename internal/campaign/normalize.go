package campaign

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// Normalize removes insignificant whitespace and sorts set-like fields. Command
// and contract order are preserved because they are user-visible execution order.
func (c *Campaign) Normalize() {
	c.Name = strings.TrimSpace(c.Name)
	c.Target.Adapter = strings.TrimSpace(c.Target.Adapter)
	c.Target.WorkingDirectory = strings.TrimSpace(c.Target.WorkingDirectory)
	c.Exploration.CancelAt = compactSorted(c.Exploration.CancelAt)
	for i := range c.Contracts {
		c.Contracts[i].Name = strings.TrimSpace(c.Contracts[i].Name)
		c.Contracts[i].Requires = compactSorted(c.Contracts[i].Requires)
		c.Contracts[i].Expression = strings.TrimSpace(c.Contracts[i].Expression)
	}
}

func compactSorted(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	slices.Sort(result)
	return result
}

// Digest returns the SHA-256 identity of the normalized campaign.
func (c Campaign) Digest() (string, error) {
	c.Normalize()
	data, err := json.Marshal(c)
	if err != nil {
		return "", fmt.Errorf("marshal normalized campaign: %w", err)
	}
	return digestJSON(data)
}

// TargetDigest identifies the adapter and command independently from the
// campaign contracts. It is stored with every attempt so a replay can reject
// evidence produced by a different target build.
func (c Campaign) TargetDigest() (string, error) {
	target := c.Target
	data, err := json.Marshal(target)
	if err != nil {
		return "", fmt.Errorf("marshal target: %w", err)
	}
	return digestJSON(data)
}

func digestJSON(data []byte) (string, error) {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}
