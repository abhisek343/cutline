package campaign

import (
	"errors"
	"strings"
	"testing"
	"time"
)

const validYAML = `
apiVersion: cutline.dev/v1alpha1
kind: Campaign
name: checkout-cancel
target:
  adapter: go-test
  command: ["go", "test", "./test/fixtures/checkout"]
exploration:
  strategy: checkpoint
  cancelAt: [" before-charge ", "before-charge", "after-charge"]
contracts:
  - name: no-charge-after-cancel
    severity: critical
    requires: ["effects.authoritative_commit", " cancellation.observed "]
    boundary: cancellation-observed
    expression: >
      !effects.exists(e, e.kind == "payment.charge" && e.committed)
`

func TestParseAppliesDefaultsAndNormalizes(t *testing.T) {
	t.Parallel()

	campaign, err := Parse([]byte(validYAML))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if campaign.Exploration.MaxSchedules != 200 {
		t.Fatalf("MaxSchedules = %d", campaign.Exploration.MaxSchedules)
	}
	if campaign.Exploration.ScheduleTimeout.Value() != 60*time.Second {
		t.Fatalf("ScheduleTimeout = %v", campaign.Exploration.ScheduleTimeout.Value())
	}
	if got, want := campaign.Exploration.CancelAt, []string{"after-charge", "before-charge"}; !equalStrings(got, want) {
		t.Fatalf("CancelAt = %v, want %v", got, want)
	}
	if got, want := campaign.Contracts[0].Requires, []string{"cancellation.observed", "effects.authoritative_commit"}; !equalStrings(got, want) {
		t.Fatalf("Requires = %v, want %v", got, want)
	}
}

func TestDigestIsStableForSetOrdering(t *testing.T) {
	t.Parallel()

	first, err := Parse([]byte(validYAML))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	second, err := Parse([]byte(strings.ReplaceAll(validYAML,
		`[" before-charge ", "before-charge", "after-charge"]`,
		`["after-charge", "before-charge"]`,
	)))
	if err != nil {
		t.Fatalf("Parse() reordered error = %v", err)
	}
	firstDigest, _ := first.Digest()
	secondDigest, _ := second.Digest()
	if firstDigest != secondDigest {
		t.Fatalf("Digest differs: %s != %s", firstDigest, secondDigest)
	}
}

func TestParseRejectsUnknownAndTrailingDocuments(t *testing.T) {
	t.Parallel()

	unknown := strings.Replace(validYAML, "name: checkout-cancel", "name: checkout-cancel\nunknown: true", 1)
	if _, err := Parse([]byte(unknown)); !errors.Is(err, ErrInvalidCampaign) {
		t.Fatalf("Parse(unknown) error = %v, want ErrInvalidCampaign", err)
	}
	if _, err := Parse([]byte(validYAML + "\n---\nname: second\n")); !errors.Is(err, ErrInvalidCampaign) {
		t.Fatalf("Parse(trailing) error = %v, want ErrInvalidCampaign", err)
	}
}

func TestParseRejectsInvalidSemantics(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"version":    strings.Replace(validYAML, APIVersionV1Alpha1, "cutline.dev/v2", 1),
		"adapter":    strings.Replace(validYAML, "adapter: go-test", "adapter: shell", 1),
		"schedules":  strings.Replace(validYAML, "strategy: checkpoint", "strategy: checkpoint\n  maxSchedules: -1", 1),
		"contract":   strings.Replace(validYAML, "severity: critical", "severity: urgent", 1),
		"expression": strings.Replace(validYAML, `expression: >`, `expression: "" #`, 1),
	}
	for name, input := range tests {
		name, input := name, input
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := Parse([]byte(input)); !IsInvalid(err) {
				t.Fatalf("Parse() error = %v, want invalid category", err)
			}
		})
	}
}

func TestParseRejectsOversizedDocument(t *testing.T) {
	t.Parallel()

	if _, err := Parse(make([]byte, maxCampaignBytes+1)); !errors.Is(err, ErrInvalidCampaign) {
		t.Fatalf("Parse(oversized) error = %v", err)
	}
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
