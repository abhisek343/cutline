package capsule

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/abhisek343/cutline/internal/buildinfo"
	"github.com/abhisek343/cutline/internal/campaign"
	"github.com/abhisek343/cutline/internal/contracts"
	"github.com/abhisek343/cutline/internal/evidence"
	"github.com/abhisek343/cutline/internal/explorer"
	"github.com/abhisek343/cutline/internal/fixtureledger"
	"github.com/abhisek343/cutline/internal/ingest"
	"github.com/abhisek343/cutline/internal/minimize"
	"github.com/abhisek343/cutline/internal/model"
	"github.com/abhisek343/cutline/internal/signature"
	"gopkg.in/yaml.v3"
)

const (
	SchemaVersion    = 1
	maxCapsuleBytes  = 100 << 20
	redactedValue    = "[REDACTED]"
	defaultRedaction = "exact values replaced with [REDACTED] before hashing"
)

type Artifact struct {
	Path      string `json:"path"`
	MediaType string `json:"mediaType"`
	SizeBytes int    `json:"sizeBytes"`
	Digest    string `json:"digest"`
}

type Manifest struct {
	SchemaVersion       int                 `json:"schemaVersion"`
	CapsuleID           string              `json:"capsuleId"`
	CreatedAt           time.Time           `json:"createdAt"`
	RunID               model.RunID         `json:"runId"`
	AttemptID           model.AttemptID     `json:"attemptId"`
	CampaignDigest      string              `json:"campaignDigest"`
	TargetDigest        string              `json:"targetDigest"`
	Adapter             string              `json:"adapter"`
	AdapterVersion      string              `json:"adapterVersion"`
	Capabilities        []string            `json:"capabilities"`
	FailureSignature    signature.Signature `json:"failureSignature"`
	Seed                int64               `json:"seed"`
	Artifacts           []Artifact          `json:"artifacts"`
	RedactionPolicy     string              `json:"redactionPolicy"`
	RedactionCount      int                 `json:"redactionCount"`
	ReplayPrerequisites []string            `json:"replayPrerequisites"`
	Stable              bool                `json:"stable"`
	ConfirmationCount   int                 `json:"confirmationCount"`
	ReproducedCount     int                 `json:"reproducedCount"`
}

type Redaction struct {
	Label string
	Value string
}

type Dependencies struct {
	Build                 buildinfo.Info `json:"build"`
	TargetDigest          string         `json:"targetDigest"`
	GoVersion             string         `json:"goVersion,omitempty"`
	Module                string         `json:"module,omitempty"`
	PostgreSQLSchema      int            `json:"postgresqlSchema,omitempty"`
	TemporalSDKVersion    string         `json:"temporalSdkVersion,omitempty"`
	TemporalServerVersion string         `json:"temporalServerVersion,omitempty"`
	Prerequisites         []string       `json:"prerequisites,omitempty"`
}

type Execution struct {
	Status   string        `json:"status"`
	ExitCode int           `json:"exitCode"`
	Duration time.Duration `json:"duration"`
}

type Input struct {
	Campaign            campaign.Campaign
	CampaignYAML        []byte
	RunID               model.RunID
	AttemptID           model.AttemptID
	Execution           Execution
	Schedule            explorer.Schedule
	Snapshot            ingest.Snapshot
	View                evidence.View
	Effects             []fixtureledger.Record
	Evaluations         []contracts.Result
	Signatures          []signature.Signature
	FailureSignature    signature.Signature
	Minimization        *minimize.Result
	BuildInfo           buildinfo.Info
	AdapterVersion      string
	Dependencies        Dependencies
	ReplayPrerequisites []string
	RedactionPolicy     string
	Redactions          []Redaction
	CreatedAt           time.Time
}

type Capsule struct {
	Manifest Manifest
	Files    map[string][]byte
}

type minimizationRecord struct {
	OriginalSchedule   explorer.Schedule  `json:"originalSchedule"`
	MinimizedSchedule  explorer.Schedule  `json:"minimizedSchedule"`
	OriginalActions    int                `json:"originalActions"`
	MinimizedActions   int                `json:"minimizedActions"`
	Attempts           []minimize.Attempt `json:"attempts"`
	MaxAttempts        int                `json:"maxAttempts"`
	Confirmations      int                `json:"confirmations"`
	ObservedSignatures []string           `json:"observedSignatures"`
	Stable             bool               `json:"stable"`
	BudgetExhausted    bool               `json:"budgetExhausted"`
	TerminationReason  string             `json:"terminationReason"`
}

type reportData struct {
	Status       string              `json:"status"`
	Signature    signature.Signature `json:"signature"`
	Schedule     explorer.Schedule   `json:"schedule"`
	View         evidence.View       `json:"view"`
	Evaluations  []contracts.Result  `json:"evaluations"`
	Minimization minimizationRecord  `json:"minimization"`
}

func Build(input Input) (Capsule, error) {
	if err := input.validate(); err != nil {
		return Capsule{}, err
	}
	campaignDigest, err := input.Campaign.Digest()
	if err != nil {
		return Capsule{}, err
	}
	targetDigest, err := input.Campaign.TargetDigest()
	if err != nil {
		return Capsule{}, err
	}
	build := input.BuildInfo
	if build.Version == "" {
		build = buildinfo.Current()
	}
	adapterVersion := input.AdapterVersion
	if adapterVersion == "" {
		adapterVersion = "native/" + input.Campaign.Target.Adapter + "/1"
	}
	deps := input.Dependencies
	if deps.Build.Version == "" {
		deps.Build = build
	}
	if deps.GoVersion == "" {
		deps.GoVersion = build.GoVersion
	}
	deps.Prerequisites = append([]string(nil), input.ReplayPrerequisites...)
	if len(deps.Prerequisites) == 0 {
		deps.Prerequisites = append([]string(nil), input.Dependencies.Prerequisites...)
	}
	minRecord := makeMinimizationRecord(input.Minimization)
	minimizedSchedule := minRecord.MinimizedSchedule
	if minimizedSchedule.ID == "" {
		minimizedSchedule = input.Schedule
		minRecord.MinimizedSchedule = minimizedSchedule
		minRecord.MinimizedActions = scheduleActions(minimizedSchedule)
	}
	files := make(map[string][]byte)
	redactions := 0
	add := func(path string, data []byte) {
		data, count := redact(data, input.Redactions)
		redactions += count
		files[path] = append([]byte(nil), data...)
	}
	campaignYAML := append([]byte(nil), input.CampaignYAML...)
	if len(campaignYAML) == 0 {
		campaignYAML, err = yaml.Marshal(input.Campaign)
		if err != nil {
			return Capsule{}, fmt.Errorf("encode campaign: %w", err)
		}
	}
	add("campaign.yaml", campaignYAML)
	addJSON := func(path string, value any) error {
		data, encodeErr := jsonBytes(value)
		if encodeErr != nil {
			return encodeErr
		}
		add(path, data)
		return nil
	}
	if err := addJSON("target.json", struct {
		Adapter          string   `json:"adapter"`
		AdapterVersion   string   `json:"adapterVersion"`
		Command          []string `json:"command"`
		WorkingDirectory string   `json:"workingDirectory,omitempty"`
		Digest           string   `json:"digest"`
	}{input.Campaign.Target.Adapter, adapterVersion, input.Campaign.Target.Command, input.Campaign.Target.WorkingDirectory, targetDigest}); err != nil {
		return Capsule{}, err
	}
	deps.TargetDigest = targetDigest
	if err := addJSON("dependencies.json", deps); err != nil {
		return Capsule{}, err
	}
	var events bytes.Buffer
	if err := ingest.WriteJSONL(&events, input.Snapshot); err != nil {
		return Capsule{}, err
	}
	add("events.jsonl", events.Bytes())
	if err := addJSON("effects.json", input.Effects); err != nil {
		return Capsule{}, err
	}
	if err := addJSON("graph.json", input.View.Edges); err != nil {
		return Capsule{}, err
	}
	if err := addJSON("evaluation.json", input.Evaluations); err != nil {
		return Capsule{}, err
	}
	if err := addJSON("schedule.json", minimizedSchedule); err != nil {
		return Capsule{}, err
	}
	if err := addJSON("minimization.json", minRecord); err != nil {
		return Capsule{}, err
	}
	data := reportData{Status: input.Execution.Status, Signature: input.failureSignature(), Schedule: minimizedSchedule, View: input.View, Evaluations: input.Evaluations, Minimization: minRecord}
	if err := addJSON("report/data.json", data); err != nil {
		return Capsule{}, err
	}
	add("replay/README.md", []byte(replayInstructions(input, adapterVersion)))
	add("report/index.html", []byte(reportHTML(data)))
	artifacts := artifactList(files)
	manifest := Manifest{SchemaVersion: SchemaVersion, CreatedAt: input.createdAt(), RunID: input.RunID, AttemptID: input.AttemptID, CampaignDigest: campaignDigest, TargetDigest: targetDigest, Adapter: input.Campaign.Target.Adapter, AdapterVersion: adapterVersion, Capabilities: append([]string(nil), input.View.Capabilities...), FailureSignature: input.failureSignature(), Seed: input.Campaign.Exploration.Seed, Artifacts: artifacts, RedactionPolicy: input.redactionPolicy(), RedactionCount: redactions, ReplayPrerequisites: append([]string(nil), deps.Prerequisites...), Stable: minRecord.Stable, ConfirmationCount: minRecord.Confirmations, ReproducedCount: countReproduced(minRecord.Attempts)}
	manifest.CapsuleID = artifactSetDigest(artifacts)
	manifestBytes, err := jsonBytes(manifest)
	if err != nil {
		return Capsule{}, err
	}
	files["manifest.json"] = manifestBytes
	files["checksums.sha256"] = checksumBytes(files)
	return Capsule{Manifest: manifest, Files: files}, nil
}

func BuildDirectory(input Input, destination string) (Manifest, error) {
	capsule, err := Build(input)
	if err != nil {
		return Manifest{}, err
	}
	if err := capsule.WriteDirectory(destination); err != nil {
		return Manifest{}, err
	}
	return capsule.Manifest, nil
}

func (c Capsule) WriteDirectory(destination string) error {
	if strings.TrimSpace(destination) == "" {
		return fmt.Errorf("capsule destination is required")
	}
	if _, err := os.Lstat(destination); err == nil {
		return fmt.Errorf("capsule destination already exists: %s", destination)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("check capsule destination: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return fmt.Errorf("create capsule parent: %w", err)
	}
	if err := os.Mkdir(destination, 0o700); err != nil {
		return fmt.Errorf("create capsule directory: %w", err)
	}
	paths := sortedKeys(c.Files)
	for _, path := range paths {
		full, err := safeJoin(destination, path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
			return fmt.Errorf("create capsule artifact directory: %w", err)
		}
		if err := os.WriteFile(full, c.Files[path], 0o600); err != nil {
			return fmt.Errorf("write capsule artifact %s: %w", path, err)
		}
	}
	return nil
}

func ValidateDirectory(root string) (Manifest, error) {
	manifestPath, err := safeFile(root, "manifest.json")
	if err != nil {
		return Manifest{}, err
	}
	manifestBytes, err := os.ReadFile(manifestPath)
	if err != nil {
		return Manifest{}, fmt.Errorf("read capsule manifest: %w", err)
	}
	var manifest Manifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode capsule manifest: %w", err)
	}
	if manifest.SchemaVersion != SchemaVersion {
		return Manifest{}, fmt.Errorf("unsupported capsule schema version %d", manifest.SchemaVersion)
	}
	if manifest.CapsuleID != artifactSetDigest(manifest.Artifacts) {
		return Manifest{}, fmt.Errorf("capsule ID does not match artifact set")
	}
	total := len(manifestBytes)
	paths := map[string]string{"manifest.json": digestBytes(manifestBytes)}
	seen := make(map[string]struct{}, len(manifest.Artifacts))
	for _, artifact := range manifest.Artifacts {
		if artifact.Path == "" || artifact.Path == "manifest.json" || artifact.Path == "checksums.sha256" {
			return Manifest{}, fmt.Errorf("invalid capsule artifact path %q", artifact.Path)
		}
		if _, ok := seen[artifact.Path]; ok {
			return Manifest{}, fmt.Errorf("duplicate capsule artifact %q", artifact.Path)
		}
		seen[artifact.Path] = struct{}{}
		path, pathErr := safeFile(root, artifact.Path)
		if pathErr != nil {
			return Manifest{}, pathErr
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return Manifest{}, fmt.Errorf("read capsule artifact %s: %w", artifact.Path, readErr)
		}
		if len(data) != artifact.SizeBytes || digestBytes(data) != artifact.Digest {
			return Manifest{}, fmt.Errorf("capsule artifact integrity failure: %s", artifact.Path)
		}
		total += len(data)
		if total > maxCapsuleBytes {
			return Manifest{}, fmt.Errorf("capsule exceeds %d bytes", maxCapsuleBytes)
		}
		paths[artifact.Path] = artifact.Digest
	}
	checksumPath, err := safeFile(root, "checksums.sha256")
	if err != nil {
		return Manifest{}, err
	}
	checksumData, err := os.ReadFile(checksumPath)
	if err != nil {
		return Manifest{}, fmt.Errorf("read capsule checksums: %w", err)
	}
	if total+len(checksumData) > maxCapsuleBytes {
		return Manifest{}, fmt.Errorf("capsule exceeds %d bytes", maxCapsuleBytes)
	}
	checksums, err := parseChecksums(checksumData)
	if err != nil {
		return Manifest{}, err
	}
	if len(checksums) != len(paths) {
		return Manifest{}, fmt.Errorf("capsule checksum set has %d entries, want %d", len(checksums), len(paths))
	}
	for path, digest := range paths {
		if checksums[path] != digest {
			return Manifest{}, fmt.Errorf("capsule checksum mismatch: %s", path)
		}
	}
	return manifest, nil
}

func (in Input) validate() error {
	if err := model.ValidateID(string(in.RunID)); err != nil {
		return fmt.Errorf("run ID: %w", err)
	}
	if err := model.ValidateID(string(in.AttemptID)); err != nil {
		return fmt.Errorf("attempt ID: %w", err)
	}
	if in.Execution.Status != "violation" {
		return fmt.Errorf("capsules require a violation result")
	}
	if len(in.Snapshot.Events) == 0 || !in.View.Complete() {
		return fmt.Errorf("capsules require complete evidence")
	}
	if len(in.Signatures) == 0 && in.FailureSignature.Digest == "" {
		return fmt.Errorf("capsules require a failure signature")
	}
	if in.Minimization == nil || !in.Minimization.Stable {
		return fmt.Errorf("capsules require stable minimization")
	}
	if in.Minimization.Target.Digest != "" && in.Minimization.Target.Digest != in.failureSignature().Digest {
		return fmt.Errorf("minimization target does not match failure signature")
	}
	return nil
}

func (in Input) failureSignature() signature.Signature {
	if in.FailureSignature.Digest != "" {
		return in.FailureSignature
	}
	return in.Signatures[0]
}

func (in Input) createdAt() time.Time {
	if in.CreatedAt.IsZero() {
		return time.Now().UTC()
	}
	return in.CreatedAt.UTC()
}

func (in Input) redactionPolicy() string {
	if strings.TrimSpace(in.RedactionPolicy) == "" {
		return defaultRedaction
	}
	return in.RedactionPolicy
}

func makeMinimizationRecord(result *minimize.Result) minimizationRecord {
	if result == nil {
		return minimizationRecord{}
	}
	record := minimizationRecord{OriginalSchedule: result.Original, MinimizedSchedule: result.Minimized, OriginalActions: scheduleActions(result.Original), MinimizedActions: scheduleActions(result.Minimized), Attempts: append([]minimize.Attempt(nil), result.Attempts...), Stable: result.Stable, BudgetExhausted: result.BudgetExhausted}
	for _, attempt := range result.Attempts {
		if attempt.SignatureDigest != "" && !contains(record.ObservedSignatures, attempt.SignatureDigest) {
			record.ObservedSignatures = append(record.ObservedSignatures, attempt.SignatureDigest)
		}
	}
	record.Confirmations = len(result.Attempts)
	switch {
	case result.Stable:
		record.TerminationReason = "confirmed"
	case result.BudgetExhausted:
		record.TerminationReason = "budget-exhausted"
	default:
		record.TerminationReason = "not-reproduced"
	}
	return record
}

func scheduleActions(schedule explorer.Schedule) int {
	return len(schedule.ReleasePrefix) + 1
}

func countReproduced(attempts []minimize.Attempt) int {
	count := 0
	for _, attempt := range attempts {
		if attempt.Reproduced {
			count++
		}
	}
	return count
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func redact(data []byte, values []Redaction) ([]byte, int) {
	count := 0
	for _, value := range values {
		if value.Value == "" {
			continue
		}
		n := bytes.Count(data, []byte(value.Value))
		if n > 0 {
			data = bytes.ReplaceAll(data, []byte(value.Value), []byte(redactedValue))
			count += n
		}
	}
	return data, count
}

func artifactList(files map[string][]byte) []Artifact {
	paths := sortedKeys(files)
	result := make([]Artifact, 0, len(paths))
	for _, path := range paths {
		if path == "manifest.json" || path == "checksums.sha256" {
			continue
		}
		result = append(result, Artifact{Path: path, MediaType: mediaType(path), SizeBytes: len(files[path]), Digest: digestBytes(files[path])})
	}
	return result
}

func artifactSetDigest(artifacts []Artifact) string {
	var value strings.Builder
	for _, artifact := range artifacts {
		value.WriteString(artifact.Path)
		value.WriteByte(0)
		value.WriteString(artifact.Digest)
		value.WriteByte(0)
	}
	return digestBytes([]byte(value.String()))
}

func checksumBytes(files map[string][]byte) []byte {
	paths := sortedKeys(files)
	var value strings.Builder
	for _, path := range paths {
		if path == "checksums.sha256" {
			continue
		}
		value.WriteString(digestBytes(files[path]))
		value.WriteString("  ")
		value.WriteString(path)
		value.WriteByte('\n')
	}
	return []byte(value.String())
}

func parseChecksums(data []byte) (map[string]string, error) {
	result := make(map[string]string)
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		parts := strings.SplitN(line, "  ", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return nil, fmt.Errorf("invalid capsule checksum line")
		}
		if _, exists := result[parts[1]]; exists {
			return nil, fmt.Errorf("duplicate capsule checksum %q", parts[1])
		}
		result[parts[1]] = parts[0]
	}
	return result, nil
}

func jsonBytes(value any) ([]byte, error) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode JSON: %w", err)
	}
	return append(data, '\n'), nil
}

func digestBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func sortedKeys(values map[string][]byte) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func mediaType(path string) string {
	switch filepath.Ext(path) {
	case ".yaml":
		return "application/yaml"
	case ".json":
		return "application/json"
	case ".jsonl":
		return "application/x-ndjson"
	case ".html":
		return "text/html; charset=utf-8"
	case ".md":
		return "text/markdown; charset=utf-8"
	default:
		return "text/plain; charset=utf-8"
	}
}

func safeJoin(root, relative string) (string, error) {
	if root == "" || relative == "" || filepath.IsAbs(relative) {
		return "", fmt.Errorf("unsafe capsule path %q", relative)
	}
	clean := filepath.Clean(relative)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("unsafe capsule path %q", relative)
	}
	full := filepath.Join(root, clean)
	base, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	full, err = filepath.Abs(full)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(base, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("unsafe capsule path %q", relative)
	}
	return full, nil
}

func safeFile(root, relative string) (string, error) {
	full, err := safeJoin(root, relative)
	if err != nil {
		return "", err
	}
	clean := filepath.Clean(relative)
	current, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	for _, part := range strings.Split(clean, string(filepath.Separator)) {
		current = filepath.Join(current, part)
		info, statErr := os.Lstat(current)
		if statErr != nil {
			return "", fmt.Errorf("inspect capsule path %s: %w", relative, statErr)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() && current == full {
			return "", fmt.Errorf("capsule path is not a regular file: %s", relative)
		}
	}
	return full, nil
}

func replayInstructions(input Input, adapterVersion string) string {
	return fmt.Sprintf("# Replay\n\nCapsule `%s` was produced by Cutline adapter `%s`.\n\nRun `cutline replay <capsule-directory>` after verifying the target and dependency digests in `manifest.json`. Exact replay requires the recorded campaign, target, and prerequisites.\n", input.failureSignature().Digest, adapterVersion)
}

func reportHTML(data reportData) string {
	status := html.EscapeString(data.Status)
	digest := html.EscapeString(data.Signature.Digest)
	contract := html.EscapeString(data.Signature.Contract)
	return "<!doctype html><meta charset=\"utf-8\"><title>Cutline failure capsule</title><main><h1>Cutline failure capsule</h1><p>Status: <strong>" + status + "</strong></p><p>Contract: " + contract + "</p><p>Failure signature: <code>" + digest + "</code></p><p>Evidence and machine-readable data are in <a href=\"data.json\">data.json</a>.</p></main>\n"
}
