package campaign

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestExecutionDigestTracksSourceWithoutExecutingTests(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles tiny local target twice")
	}
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "go.mod"), []byte("module identity.test/fixture\ngo 1.26\n"), 0600); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(directory, "target_test.go")
	write := func(value string) {
		t.Helper()
		code := "package fixture\nimport (\"testing\"; \"os\")\nfunc TestTarget(t *testing.T) { os.WriteFile(\"executed\", []byte(\"" + value + "\"),0600) }\n"
		if err := os.WriteFile(source, []byte(code), 0600); err != nil {
			t.Fatal(err)
		}
	}
	spec := Campaign{Target: TargetSpec{Adapter: "go-test", Command: []string{"go", "test", ".", "-run", "TestTarget", "-count=1"}}}
	write("first")
	first, err := spec.ExecutionDigest(context.Background(), directory)
	if err != nil {
		t.Fatal(err)
	}
	same, err := spec.ExecutionDigest(context.Background(), directory)
	if err != nil || first != same {
		t.Fatalf("unchanged build identity first=%s second=%s error=%v", first, same, err)
	}
	write("changed")
	changed, err := spec.ExecutionDigest(context.Background(), directory)
	if err != nil || first == changed {
		t.Fatalf("source change not detected first=%s changed=%s error=%v", first, changed, err)
	}
	if _, err := os.Stat(filepath.Join(directory, "executed")); !os.IsNotExist(err) {
		t.Fatalf("provenance compiled target executed test: %v", err)
	}
}

func TestExecutionDigestRefusesOpaqueScriptsAndFileArguments(t *testing.T) {
	directory := t.TempDir()
	script := filepath.Join(directory, "target.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, command := range [][]string{{"./target.sh"}, {"/bin/sh", "target.sh"}} {
		spec := Campaign{Target: TargetSpec{Adapter: "go-command", Command: command}}
		if _, err := spec.ExecutionDigest(context.Background(), directory); err == nil {
			t.Fatalf("accepted opaque script command=%v", command)
		}
	}
}

func TestGoTestProvenanceRefusesOpaqueLauncher(t *testing.T) {
	spec := Campaign{Target: TargetSpec{Adapter: "go-test", Command: []string{"true"}}}
	if _, err := spec.ExecutionDigest(context.Background(), t.TempDir()); err == nil {
		t.Fatal("go-test target claimed source identity from opaque launcher alone")
	}
}
