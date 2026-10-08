package campaign

import (
	"context"
	"crypto/sha256"
	"debug/elf"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ExecutionDigest identifies the current local target build without running it.
// Go test commands are compiled; other commands must be ELF executables. Opaque
// interpreter commands are refused because hashing their launcher misses code.
// This identifies build content, not external dependencies or mutable input data.
func (c Campaign) ExecutionDigest(ctx context.Context, base string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if base == "" {
		var err error
		base, err = os.Getwd()
		if err != nil {
			return "", err
		}
	}
	base, err := filepath.Abs(base)
	if err != nil {
		return "", err
	}
	work := filepath.Join(base, c.Target.WorkingDirectory)
	rel, err := filepath.Rel(base, work)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("target working directory escapes base")
	}
	command := append([]string(nil), c.Target.Command...)
	if len(command) == 0 {
		return "", fmt.Errorf("target command is empty")
	}
	env := []string{}
	for _, key := range []string{"PATH", "HOME", "TMPDIR", "GOCACHE", "GOPATH", "GOMODCACHE", "GOTOOLCHAIN", "GOENV", "CGO_ENABLED", "CC", "CXX"} {
		if value, ok := os.LookupEnv(key); ok {
			env = append(env, key+"="+value)
		}
	}
	hash := sha256.New()
	descriptor, err := c.TargetDigest()
	if err != nil {
		return "", err
	}
	_, _ = io.WriteString(hash, descriptor+"\x00")
	if filepath.Base(command[0]) == "env" {
		launcher, err := exec.LookPath(command[0])
		if err != nil {
			return "", fmt.Errorf("resolve target launcher: %w", err)
		}
		if err := hashExecutable(hash, launcher); err != nil {
			return "", err
		}
		command = command[1:]
		for len(command) > 0 && strings.Contains(command[0], "=") && !strings.HasPrefix(command[0], "-") {
			env = append(env, command[0])
			command = command[1:]
		}
		if len(command) == 0 {
			return "", fmt.Errorf("target env command has no executable")
		}
	}
	executable := command[0]
	if strings.ContainsRune(executable, filepath.Separator) && !filepath.IsAbs(executable) {
		executable = filepath.Join(work, executable)
	} else {
		executable, err = exec.LookPath(executable)
		if err != nil {
			return "", fmt.Errorf("resolve target executable: %w", err)
		}
	}
	if c.Target.Adapter == "go-test" && filepath.Base(executable) != "go" {
		return "", fmt.Errorf("go-test provenance requires a direct go test command; opaque launchers are unsupported")
	}
	name := filepath.Base(executable)
	if strings.HasPrefix(name, "python") || name == "sh" || name == "bash" || name == "dash" || name == "zsh" || name == "ksh" || name == "ruby" || name == "perl" || name == "node" || name == "deno" || name == "bun" || name == "java" {
		return "", fmt.Errorf("target provenance cannot establish imported code for interpreter %q; use a compiled target", name)
	}
	if filepath.Base(executable) == "go" {
		if len(command) < 3 || command[1] != "test" {
			return "", fmt.Errorf("target provenance supports go test only; use a compiled executable for other Go commands")
		}
		directory, err := os.MkdirTemp("", "cutline-build-identity-")
		if err != nil {
			return "", err
		}
		defer os.RemoveAll(directory)
		binary := filepath.Join(directory, "target.test")
		args := []string{"test", "-buildvcs=false", "-c", "-o", binary}
		for index := 2; index < len(command); index++ {
			arg := command[index]
			if arg == "-args" {
				break
			}
			if arg == "-o" || strings.HasPrefix(arg, "-o=") || arg == "-c" || arg == "-exec" || strings.HasPrefix(arg, "-exec=") {
				return "", fmt.Errorf("target provenance does not support Go flag %q", arg)
			}
			args = append(args, arg)
		}
		build := exec.CommandContext(ctx, executable, args...)
		build.Dir, build.Env = work, env
		if output, err := build.CombinedOutput(); err != nil {
			return "", fmt.Errorf("compile target identity: %w: %s", err, strings.TrimSpace(string(output)))
		}
		if err := hashExecutable(hash, binary); err != nil {
			return "", err
		}
	} else {
		if err := hashExecutable(hash, executable); err != nil {
			return "", err
		}
		for _, arg := range command[1:] {
			path := arg
			if !filepath.IsAbs(path) {
				path = filepath.Join(work, path)
			}
			if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
				return "", fmt.Errorf("target argument %q names a file; executable-only provenance cannot establish its code or input identity", arg)
			}
		}
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil)), nil
}

func hashExecutable(hash io.Writer, path string) error {
	binary, err := elf.Open(path)
	if err != nil {
		return fmt.Errorf("target provenance requires an ELF executable (%s): %w", path, err)
	}
	_ = binary.Close()
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err := io.Copy(hash, file); err != nil {
		return err
	}
	return nil
}
