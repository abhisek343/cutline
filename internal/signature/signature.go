package signature

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/abhisek343/cutline/internal/campaign"
	"github.com/abhisek343/cutline/internal/contracts"
	"github.com/abhisek343/cutline/internal/evidence"
	"github.com/abhisek343/cutline/internal/model"
)

const Version = 2

var ErrUnsupportedWitness = errors.New("minimization requires a supported, stable effect witness")

type BuildContext struct {
	AdapterMajorVersion string
	SchemaMajorVersion  int
}

type Signature struct {
	Version              int      `json:"version"`
	Digest               string   `json:"digest"`
	Contract             string   `json:"contract"`
	ContractVersion      int      `json:"contractVersion"`
	ContractFingerprint  string   `json:"contractFingerprint,omitempty"`
	ViolationClass       string   `json:"violationClass"`
	PrimaryEntityKind    string   `json:"primaryEntityKind"`
	PrimaryIdentityClass string   `json:"primaryIdentityClass"`
	PrimaryIdentityKey   string   `json:"primaryIdentityKey,omitempty"`
	CancellationTrigger  string   `json:"cancellationTrigger"`
	CausalPath           []string `json:"causalPath"`
	AdapterMajorVersion  string   `json:"adapterMajorVersion"`
	SchemaMajorVersion   int      `json:"schemaMajorVersion"`
}

// BuildAll preserves campaign contract order and emits only stable identities
// for violating evaluations. Duplicate digests are collapsed.
func BuildAll(view evidence.View, specs []campaign.ContractSpec, evaluations []contracts.Result, context BuildContext) ([]Signature, error) {
	byContract := make(map[string]contracts.Result, len(evaluations))
	for _, evaluation := range evaluations {
		byContract[evaluation.Contract] = evaluation
	}
	result := make([]Signature, 0)
	seen := make(map[string]struct{})
	for _, spec := range specs {
		evaluation, ok := byContract[spec.Name]
		if !ok || evaluation.Status != contracts.StatusViolation {
			continue
		}
		value, err := Build(view, spec, evaluation, context)
		if errors.Is(err, ErrUnsupportedWitness) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if _, ok := seen[value.Digest]; ok {
			continue
		}
		seen[value.Digest] = struct{}{}
		result = append(result, value)
	}
	return result, nil
}

func Build(view evidence.View, spec campaign.ContractSpec, evaluation contracts.Result, context BuildContext) (Signature, error) {
	if evaluation.Status != contracts.StatusViolation {
		return Signature{}, fmt.Errorf("cannot sign non-violation status %q", evaluation.Status)
	}
	contractVersion := spec.Version
	if contractVersion == 0 {
		contractVersion = 1
	}
	if context.SchemaMajorVersion <= 0 {
		context.SchemaMajorVersion = model.EventSchemaVersion
	}
	if strings.TrimSpace(context.AdapterMajorVersion) == "" {
		context.AdapterMajorVersion = "unknown/1"
	}
	cancellation := firstCancellation(view)
	effect := primaryEntity(view, evaluation)
	if effect == nil || strings.TrimSpace(effect.IdempotencyKey) == "" {
		return Signature{}, fmt.Errorf("%w: contract %q", ErrUnsupportedWitness, spec.Name)
	}
	violationClass := classify(evaluation, effect)
	path := normalizedPath(view, effect.OwnerTaskID, cancellationID(cancellation))
	result := Signature{
		Version:              Version,
		Contract:             spec.Name,
		ContractVersion:      contractVersion,
		ContractFingerprint:  contractFingerprint(spec),
		PrimaryIdentityKey:   hashText(effect.EvidenceSource + "\x00" + effect.IdempotencyKey),
		ViolationClass:       violationClass,
		PrimaryEntityKind:    "effect",
		PrimaryIdentityClass: stableKind(effect.Kind),
		CancellationTrigger:  cancellationTrigger(cancellation),
		CausalPath:           path,
		AdapterMajorVersion:  context.AdapterMajorVersion,
		SchemaMajorVersion:   context.SchemaMajorVersion,
	}
	digest, err := digest(result)
	if err != nil {
		return Signature{}, err
	}
	result.Digest = digest
	return result, nil
}

// A contract evaluator must supply the actual counterexample. Guessing the
// first effect after a boundary can silently change a minimized failure.
func primaryEntity(view evidence.View, evaluation contracts.Result) *evidence.Effect {
	if evaluation.OffendingEffectID == "" {
		return nil
	}
	for index := range view.Effects {
		if view.Effects[index].ID == evaluation.OffendingEffectID {
			return &view.Effects[index]
		}
	}
	return nil
}

func contractFingerprint(spec campaign.ContractSpec) string {
	requires := append([]string{}, spec.Requires...)
	sort.Strings(requires)
	data, _ := json.Marshal(struct {
		Expression string   `json:"expression"`
		Boundary   string   `json:"boundary"`
		Requires   []string `json:"requires"`
	}{strings.TrimSpace(spec.Expression), spec.Boundary, requires})
	return hashText(string(data))
}

func hashText(value string) string {
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func classify(evaluation contracts.Result, effect *evidence.Effect) string {
	if effect != nil && evaluation.BoundaryOrder > 0 {
		if effect.AttemptOrder > evaluation.BoundaryOrder {
			return "post-cancel-effect-attempt"
		}
		if effect.CommitOrder > evaluation.BoundaryOrder {
			return "post-cancel-effect-commit"
		}
	}
	return "contract-violation"
}

func firstCancellation(view evidence.View) *evidence.Cancellation {
	if len(view.Cancellations) == 0 {
		return nil
	}
	cancellations := append([]evidence.Cancellation(nil), view.Cancellations...)
	sort.SliceStable(cancellations, func(left, right int) bool {
		leftOrder := cancellations[left].ObservedOrder
		if leftOrder == 0 {
			leftOrder = cancellations[left].RequestedOrder
		}
		rightOrder := cancellations[right].ObservedOrder
		if rightOrder == 0 {
			rightOrder = cancellations[right].RequestedOrder
		}
		return leftOrder < rightOrder
	})
	return &cancellations[0]
}

func cancellationID(cancellation *evidence.Cancellation) string {
	if cancellation == nil {
		return ""
	}
	return cancellation.ID
}

func cancellationTrigger(cancellation *evidence.Cancellation) string {
	if cancellation == nil || strings.TrimSpace(cancellation.Trigger) == "" {
		return "none"
	}
	return stableKind(cancellation.Trigger)
}

func normalizedPath(view evidence.View, from, to string) []string {
	if from == "" || to == "" {
		return []string{}
	}
	path := view.Path(from, to)
	if len(path) == 0 {
		return []string{nodeClass(from), nodeClass(to)}
	}
	result := make([]string, 0, len(path)*2-1)
	for index, node := range path {
		if index > 0 {
			result = append(result, edgeRelation(view, path[index-1], node))
		}
		result = append(result, nodeClass(node))
	}
	return result
}

func edgeRelation(view evidence.View, from, to string) string {
	for _, edge := range view.Edges {
		if edge.From == from && edge.To == to {
			return edge.Relation
		}
	}
	return "causal-edge"
}

func nodeClass(value string) string {
	prefix, _, ok := strings.Cut(value, "_")
	if !ok || prefix == "" {
		return "entity"
	}
	switch prefix {
	case "cancel":
		return "cancellation"
	case "visit":
		return "checkpoint"
	case "task", "effect", "resource", "session":
		return prefix
	default:
		return "entity"
	}
}

func stableKind(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "unknown"
	}
	return value
}

func digest(value Signature) (string, error) {
	value.Digest = ""
	data, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("marshal failure signature: %w", err)
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}
