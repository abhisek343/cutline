package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func execute(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command := New(&stdout, &stderr)
	command.SetArgs(args)
	err := command.Execute()
	return stdout.String(), stderr.String(), err
}

func TestDoctorJSON(t *testing.T) {
	t.Parallel()

	stdout, _, err := execute(t, "doctor", "--json")
	if err != nil {
		t.Fatalf("doctor error = %v", err)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("doctor output is not JSON: %v\n%s", err, stdout)
	}
	if _, ok := result["build"]; !ok {
		t.Fatalf("doctor output missing build: %v", result)
	}
}

func TestRunValidatesCampaign(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "campaign.yaml")
	content := `
apiVersion: cutline.dev/v1alpha1
kind: Campaign
name: test
target:
  adapter: go-test
  command: ["go", "test", "./fixture"]
contracts:
  - name: no-effect
    severity: critical
    expression: "true"
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout, _, err := execute(t, "run", "--campaign", path, "--validate-only")
	if err != nil {
		t.Fatalf("run error = %v", err)
	}
	if !strings.Contains(stdout, `campaign "test" validated`) {
		t.Fatalf("run output = %q", stdout)
	}
}

func TestExitCodes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		err  error
		code int
	}{
		{nil, 0},
		{ErrViolation, 2},
		{ErrInconclusive, 3},
		{ErrInvalidRun, 4},
		{errors.New("boom"), 1},
	}
	for _, tt := range tests {
		if got := ExitCode(tt.err); got != tt.code {
			t.Errorf("ExitCode(%v) = %d, want %d", tt.err, got, tt.code)
		}
	}
}

func TestRunRejectsUnknownField(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "campaign.yaml")
	if err := os.WriteFile(path, []byte("apiVersion: bad\nunknown: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := execute(t, "run", "--campaign", path); err == nil {
		t.Fatal("run unexpectedly accepted invalid campaign")
	}
}

func TestPlaceholderCommandsAreExplicit(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"minimize", "replay", "report"} {
		_, _, err := execute(t, name)
		if !errors.Is(err, ErrNotImplemented) {
			t.Errorf("%s error = %v, want ErrNotImplemented", name, err)
		}
	}
}
