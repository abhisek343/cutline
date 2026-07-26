package native

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/abhisek343/cutline/internal/campaign"
	"github.com/abhisek343/cutline/internal/contracts"
	"github.com/abhisek343/cutline/internal/control"
	"github.com/abhisek343/cutline/internal/evidence"
	"github.com/abhisek343/cutline/internal/fixtureledger"
	"github.com/abhisek343/cutline/internal/ingest"
	"github.com/abhisek343/cutline/internal/ledger"
	"github.com/abhisek343/cutline/internal/model"
	"github.com/abhisek343/cutline/internal/scheduler"
)

const (
	maxProcessOutput = 1 << 20
	maxEvents        = 100_000
	maxLedgerRecords = 10_000
)

var (
	ErrUnsupportedAdapter = errors.New("unsupported native adapter")
)

type OverallStatus string

const (
	OverallPass         OverallStatus = "pass"
	OverallViolation    OverallStatus = "violation"
	OverallInconclusive OverallStatus = "inconclusive"
	OverallInvalid      OverallStatus = "invalid"
)

type Result struct {
	RunID       model.RunID            `json:"runId"`
	AttemptID   model.AttemptID        `json:"attemptId"`
	Status      OverallStatus          `json:"status"`
	Evaluations []contracts.Result     `json:"evaluations"`
	Evidence    ingest.Snapshot        `json:"evidence"`
	View        evidence.View          `json:"view"`
	Effects     []fixtureledger.Record `json:"effects"`
	Stdout      string                 `json:"stdout,omitempty"`
	Stderr      string                 `json:"stderr,omitempty"`
	ExitCode    int                    `json:"exitCode"`
	Duration    time.Duration          `json:"duration"`
}

type Runner struct {
	BaseDirectory string
	TempDirectory string
	StoreFactory  func(context.Context, model.RunID, model.AttemptID, int) (ingest.Store, error)
}

func (r Runner) Run(ctx context.Context, spec campaign.Campaign) (Result, error) {
	if spec.Target.Adapter != "go-test" && spec.Target.Adapter != "go-command" {
		return Result{}, ErrUnsupportedAdapter
	}
	if len(spec.Exploration.CancelAt) == 0 {
		return Result{}, fmt.Errorf("native run requires exploration.cancelAt")
	}
	runValue, err := model.NewID("run")
	if err != nil {
		return Result{}, err
	}
	attemptValue, err := model.NewID("attempt")
	if err != nil {
		return Result{}, err
	}
	sessionValue, err := model.NewID("session")
	if err != nil {
		return Result{}, err
	}
	token, err := randomToken()
	if err != nil {
		return Result{}, err
	}
	runID := model.RunID(runValue)
	attemptID := model.AttemptID(attemptValue)

	tempRoot := r.TempDirectory
	if tempRoot == "" {
		tempRoot = os.TempDir()
	}
	runDir, err := os.MkdirTemp(tempRoot, "cutline-run-")
	if err != nil {
		return Result{}, fmt.Errorf("create run directory: %w", err)
	}
	defer os.RemoveAll(runDir)

	socketPath := filepath.Join(runDir, "control.sock")
	ledgerPath := filepath.Join(runDir, "fixture-effects.jsonl")
	var store ingest.Store = ingest.NewMemory(runID, attemptID, maxEvents)
	if r.StoreFactory != nil {
		store, err = r.StoreFactory(ctx, runID, attemptID, maxEvents)
		if err != nil {
			return Result{}, fmt.Errorf("create evidence store: %w", err)
		}
	}
	if closer, ok := store.(interface{ Close() }); ok {
		defer closer.Close()
	}
	singleCut, err := scheduler.NewSingleCut(spec.Exploration.CancelAt[0])
	if err != nil {
		return Result{}, err
	}
	handler, err := newEventHandler(runID, attemptID, store, singleCut)
	if err != nil {
		return Result{}, err
	}
	server, err := control.Listen(control.ServerConfig{
		Network:   "unix",
		Address:   socketPath,
		Token:     token,
		RunID:     runID,
		AttemptID: attemptID,
		Handler:   handler,
	})
	if err != nil {
		server, err = control.Listen(control.ServerConfig{
			Network:   "tcp",
			Address:   "127.0.0.1:0",
			Token:     token,
			RunID:     runID,
			AttemptID: attemptID,
			Handler:   handler,
		})
		if err != nil {
			return Result{}, fmt.Errorf("start Unix and TCP control endpoints: %w", err)
		}
	}
	controlNetwork, controlAddress := server.Endpoint()

	runCtx, cancel := context.WithTimeout(ctx, spec.Exploration.ScheduleTimeout.Value())
	defer cancel()
	command := exec.CommandContext(runCtx, spec.Target.Command[0], spec.Target.Command[1:]...)
	command.Dir, err = r.resolveWorkingDirectory(spec.Target.WorkingDirectory)
	if err != nil {
		_ = server.Close()
		return Result{}, err
	}
	command.Env = targetEnvironment(map[string]string{
		"CUTLINE_NETWORK":        controlNetwork,
		"CUTLINE_ENDPOINT":       controlAddress,
		"CUTLINE_TOKEN":          token,
		"CUTLINE_RUN_ID":         string(runID),
		"CUTLINE_ATTEMPT_ID":     string(attemptID),
		"CUTLINE_SESSION_ID":     sessionValue,
		"CUTLINE_FIXTURE_LEDGER": ledgerPath,
	})
	stdout := &limitedBuffer{limit: maxProcessOutput}
	stderr := &limitedBuffer{limit: maxProcessOutput}
	command.Stdout = stdout
	command.Stderr = stderr

	started := time.Now()
	runErr := command.Run()
	duration := time.Since(started)
	if closeErr := server.Close(); closeErr != nil {
		store.MarkIncomplete("close control server: " + closeErr.Error())
	}
	if runErr != nil {
		store.MarkIncomplete("target process: " + runErr.Error())
	}
	if !singleCut.Injected() {
		store.MarkIncomplete("planned cancellation point was not reached")
	}

	records, ledgerErr := fixtureledger.Read(ledgerPath, maxLedgerRecords)
	if ledgerErr != nil {
		store.MarkIncomplete("fixture ledger: " + ledgerErr.Error())
		records = nil
	}
	authoritativeComplete := ledgerErr == nil
	if derived, ok := store.(ledger.DerivedStore); ok {
		if err := derived.RecordAuthoritativeEffects(records); err != nil {
			_ = store.MarkIncomplete("persist authoritative effects: " + err.Error())
			authoritativeComplete = false
		}
	}
	markMissingTerminalEvidence(store)
	snapshot, freezeErr := store.Freeze()
	if freezeErr != nil {
		return Result{}, fmt.Errorf("freeze evidence: %w", freezeErr)
	}
	view := evidence.Build(evidence.BuildInput{Snapshot: snapshot, AuthoritativeEffects: records, AuthoritativeEffectsComplete: authoritativeComplete})
	if derived, ok := store.(ledger.DerivedStore); ok {
		if err := derived.PersistGraph(view.Edges); err != nil {
			view.Issues = append(view.Issues, evidence.Issue{Code: "graph_persist", Message: err.Error()})
		}
	}
	evaluations := contracts.Evaluate(view, spec.Contracts)

	result := Result{
		RunID:       runID,
		AttemptID:   attemptID,
		Status:      overallStatus(view, evaluations),
		Evaluations: evaluations,
		Evidence:    snapshot,
		View:        view,
		Effects:     records,
		Stdout:      stdout.String(),
		Stderr:      stderr.String(),
		ExitCode:    exitCode(runErr),
		Duration:    duration,
	}
	if runErr != nil && result.Status == OverallPass {
		result.Status = OverallInconclusive
	}
	return result, nil
}

func (r Runner) resolveWorkingDirectory(relative string) (string, error) {
	base := r.BaseDirectory
	if base == "" {
		var err error
		base, err = os.Getwd()
		if err != nil {
			return "", fmt.Errorf("get working directory: %w", err)
		}
	}
	base, err := filepath.Abs(base)
	if err != nil {
		return "", fmt.Errorf("resolve base directory: %w", err)
	}
	target := base
	if relative != "" {
		target = filepath.Join(base, relative)
	}
	target, err = filepath.Abs(target)
	if err != nil {
		return "", fmt.Errorf("resolve target working directory: %w", err)
	}
	rel, err := filepath.Rel(base, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("target working directory escapes base")
	}
	return target, nil
}

func targetEnvironment(extra map[string]string) []string {
	allowed := []string{
		"PATH", "HOME", "TMPDIR", "GOCACHE", "GOPATH", "GOMODCACHE",
		"GOTOOLCHAIN", "GOENV", "CGO_ENABLED", "CC", "CXX",
	}
	environment := make([]string, 0, len(allowed)+len(extra))
	for _, key := range allowed {
		if value, ok := os.LookupEnv(key); ok {
			environment = append(environment, key+"="+value)
		}
	}
	keys := make([]string, 0, len(extra))
	for key := range extra {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		environment = append(environment, key+"="+extra[key])
	}
	return environment
}

func markMissingTerminalEvidence(store ingest.Store) {
	events, err := store.Current()
	if err != nil {
		_ = store.MarkIncomplete("read current evidence: " + err.Error())
		return
	}
	required := map[model.EventType]bool{
		model.EventCancelObserved: false,
		model.EventTargetReturned: false,
		model.EventDrainCompleted: false,
		model.EventSessionEnded:   false,
	}
	for _, event := range events {
		if _, ok := required[event.Type]; ok {
			required[event.Type] = true
		}
	}
	for eventType, found := range required {
		if !found {
			store.MarkIncomplete("missing required event: " + string(eventType))
		}
	}
}

func overallStatus(view evidence.View, evaluations []contracts.Result) OverallStatus {
	if !view.Complete() {
		return OverallInconclusive
	}
	status := OverallPass
	for _, evaluation := range evaluations {
		switch evaluation.Status {
		case contracts.StatusInvalid:
			return OverallInvalid
		case contracts.StatusInconclusive:
			status = OverallInconclusive
		case contracts.StatusViolation:
			if status != OverallInconclusive {
				status = OverallViolation
			}
		}
	}
	return status
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}

func randomToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate control token: %w", err)
	}
	return hex.EncodeToString(raw), nil
}

type limitedBuffer struct {
	buffer bytes.Buffer
	limit  int
}

func (b *limitedBuffer) Write(data []byte) (int, error) {
	original := len(data)
	remaining := b.limit - b.buffer.Len()
	if remaining > 0 {
		_, _ = b.buffer.Write(data[:min(remaining, len(data))])
	}
	return original, nil
}

func (b *limitedBuffer) String() string {
	return b.buffer.String()
}

var _ io.Writer = (*limitedBuffer)(nil)
