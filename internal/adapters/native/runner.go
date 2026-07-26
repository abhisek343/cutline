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
	"github.com/abhisek343/cutline/internal/explorer"
	"github.com/abhisek343/cutline/internal/fixtureledger"
	"github.com/abhisek343/cutline/internal/ingest"
	"github.com/abhisek343/cutline/internal/ledger"
	"github.com/abhisek343/cutline/internal/minimize"
	"github.com/abhisek343/cutline/internal/model"
	"github.com/abhisek343/cutline/internal/scheduler"
	"github.com/abhisek343/cutline/internal/signature"
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
	Signatures  []signature.Signature  `json:"signatures,omitempty"`
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

type CampaignResult struct {
	Status     OverallStatus         `json:"status"`
	Discovery  Result                `json:"discovery"`
	Plan       explorer.Plan         `json:"plan"`
	Schedules  []Result              `json:"schedules"`
	Signatures []signature.Signature `json:"signatures,omitempty"`
}

func (r Runner) Run(ctx context.Context, spec campaign.Campaign) (Result, error) {
	if len(spec.Exploration.CancelAt) == 0 {
		return Result{}, fmt.Errorf("native run requires exploration.cancelAt")
	}
	policy, err := scheduler.NewSingleCut(spec.Exploration.CancelAt[0])
	if err != nil {
		return Result{}, err
	}
	return r.runWithPolicy(ctx, spec, policy, false)
}

// Discover performs a cancellation-free pass so schedule generation is based
// on observed checkpoints rather than campaign text or target logs.
func (r Runner) Discover(ctx context.Context, spec campaign.Campaign) (Result, explorer.Discovery, error) {
	policy := scheduler.NewDiscovery(spec.Exploration.MaxPointVisits)
	result, err := r.runWithPolicy(ctx, spec, policy, true)
	if err != nil {
		return Result{}, explorer.Discovery{}, err
	}
	discovery := explorer.Discover(result.Evidence.Events, result.Evidence.IncompleteReasons, spec.Exploration.MaxPointVisits)
	if policy.Truncated() {
		discovery.Truncated = true
	}
	for _, issue := range result.View.Issues {
		reason := "discovery evidence issue: " + issue.Code + ": " + issue.Message
		if !slices.Contains(discovery.IncompleteReasons, reason) {
			discovery.IncompleteReasons = append(discovery.IncompleteReasons, reason)
		}
	}
	return result, discovery, nil
}

// RunCampaign discovers the target once and then executes every bounded
// single-cut schedule in discovery order.
func (r Runner) RunCampaign(ctx context.Context, spec campaign.Campaign) (CampaignResult, error) {
	discoveryResult, discovery, err := r.Discover(ctx, spec)
	if err != nil {
		return CampaignResult{}, err
	}
	plan, err := explorer.Enumerate(spec.Exploration.Strategy, discovery, spec.Exploration.CancelAt, spec.Exploration.MaxSchedules, spec.Exploration.PrefixDepth)
	if err != nil {
		return CampaignResult{}, err
	}
	result := CampaignResult{Discovery: discoveryResult, Plan: plan, Schedules: []Result{}}
	for _, schedule := range plan.Schedules {
		scheduleSpec := spec
		scheduleSpec.Exploration.Strategy = "single-cut"
		scheduleSpec.Exploration.CancelAt = []string{schedule.CancelAt}
		scheduleSpec.Exploration.MaxSchedules = 1
		scheduleResult, runErr := r.RunSchedule(ctx, scheduleSpec, schedule)
		if runErr != nil {
			return CampaignResult{}, fmt.Errorf("run schedule %s: %w", schedule.ID, runErr)
		}
		result.Schedules = append(result.Schedules, scheduleResult)
	}
	result.Status = overallCampaignStatus(plan, result.Schedules)
	result.Signatures = uniqueSignatures(result.Schedules)
	return result, nil
}

func (r Runner) RunSchedule(ctx context.Context, spec campaign.Campaign, schedule explorer.Schedule) (Result, error) {
	policy, err := scheduler.NewPrefixCut(schedule.CancelAt, schedule.ReleasePrefix)
	if err != nil {
		return Result{}, err
	}
	spec.Exploration.CancelAt = []string{schedule.CancelAt}
	return r.runWithPolicy(ctx, spec, policy, false)
}

func (r Runner) MinimizeSchedule(ctx context.Context, spec campaign.Campaign, schedule explorer.Schedule, target signature.Signature, config minimize.Config) (minimize.Result, error) {
	return minimize.Minimize(ctx, schedule, target, config, func(candidateContext context.Context, candidate explorer.Schedule) ([]signature.Signature, error) {
		result, err := r.RunSchedule(candidateContext, spec, candidate)
		if err != nil {
			return nil, err
		}
		return result.Signatures, nil
	})
}

func (r Runner) runWithPolicy(ctx context.Context, spec campaign.Campaign, policy scheduler.Policy, discovery bool) (Result, error) {
	if spec.Target.Adapter != "go-test" && spec.Target.Adapter != "go-command" {
		return Result{}, ErrUnsupportedAdapter
	}
	if policy == nil {
		return Result{}, fmt.Errorf("native run requires a scheduler policy")
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
	handler, err := newEventHandler(runID, attemptID, store, policy)
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
	targetExtra := map[string]string{
		"CUTLINE_NETWORK":        controlNetwork,
		"CUTLINE_ENDPOINT":       controlAddress,
		"CUTLINE_TOKEN":          token,
		"CUTLINE_RUN_ID":         string(runID),
		"CUTLINE_ATTEMPT_ID":     string(attemptID),
		"CUTLINE_SESSION_ID":     sessionValue,
		"CUTLINE_FIXTURE_LEDGER": ledgerPath,
	}
	if discovery {
		targetExtra["CUTLINE_DISCOVERY"] = "1"
	}
	command.Env = targetEnvironment(targetExtra)
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
	if !discovery && !policy.Injected() {
		store.MarkIncomplete("planned cancellation point was not reached")
	}
	if bounded, ok := policy.(interface{ Truncated() bool }); ok && bounded.Truncated() {
		store.MarkIncomplete("maximum checkpoint visits reached")
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
	markMissingTerminalEvidence(store, !discovery)
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
	evaluations := []contracts.Result{}
	if !discovery {
		evaluations = contracts.Evaluate(view, spec.Contracts)
	}
	signatures := []signature.Signature{}
	if !discovery {
		var signatureErr error
		signatures, signatureErr = signature.BuildAll(view, spec.Contracts, evaluations, signature.BuildContext{
			AdapterMajorVersion: "native/" + spec.Target.Adapter + "/1",
			SchemaMajorVersion:  model.EventSchemaVersion,
		})
		if signatureErr != nil {
			return Result{}, fmt.Errorf("build failure signature: %w", signatureErr)
		}
	}

	result := Result{
		RunID:       runID,
		AttemptID:   attemptID,
		Status:      overallStatus(view, evaluations),
		Evaluations: evaluations,
		Signatures:  signatures,
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

func markMissingTerminalEvidence(store ingest.Store, requireCancellation bool) {
	events, err := store.Current()
	if err != nil {
		_ = store.MarkIncomplete("read current evidence: " + err.Error())
		return
	}
	required := map[model.EventType]bool{
		model.EventTargetReturned: false,
		model.EventDrainCompleted: false,
		model.EventSessionEnded:   false,
	}
	if requireCancellation {
		required[model.EventCancelObserved] = false
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

func overallCampaignStatus(plan explorer.Plan, schedules []Result) OverallStatus {
	inconclusive := len(schedules) == 0 || len(plan.DiscoveryIncomplete) > 0 || plan.DiscoveryTruncated || len(plan.Unreachable) > 0
	invalid := false
	violation := false
	for _, schedule := range schedules {
		switch schedule.Status {
		case OverallInvalid:
			invalid = true
		case OverallInconclusive:
			inconclusive = true
		case OverallViolation:
			violation = true
		}
	}
	if inconclusive {
		return OverallInconclusive
	}
	if invalid {
		return OverallInvalid
	}
	if violation {
		return OverallViolation
	}
	return OverallPass
}

func uniqueSignatures(schedules []Result) []signature.Signature {
	result := make([]signature.Signature, 0)
	seen := make(map[string]struct{})
	for _, schedule := range schedules {
		for _, value := range schedule.Signatures {
			if _, ok := seen[value.Digest]; ok {
				continue
			}
			seen[value.Digest] = struct{}{}
			result = append(result, value)
		}
	}
	return result
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
