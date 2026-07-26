package signature

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/abhisek343/cutline/internal/campaign"
	"github.com/abhisek343/cutline/internal/contracts"
	"github.com/abhisek343/cutline/internal/evidence"
	"github.com/abhisek343/cutline/internal/model"
)

const Version = 1

type BuildContext struct {
	AdapterMajorVersion string
	SchemaMajorVersion  int
}

type Signature struct {
	Version              int      `json:"version"`
	Digest               string   `json:"digest"`
	Contract             string   `json:"contract"`
	ContractVersion      int      `json:"contractVersion"`
	ViolationClass       string   `json:"violationClass"`
	PrimaryEntityKind    string   `json:"primaryEntityKind"`
	PrimaryIdentityClass string   `json:"primaryIdentityClass"`
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
	effect, task := primaryEntity(view, evaluation)
	primaryKind := "none"
	primaryClass := "none"
	pathFrom := ""
	if effect != nil {
		primaryKind = "effect"
		primaryClass = stableKind(effect.Kind)
		pathFrom = effect.OwnerTaskID
	} else if task != nil {
		primaryKind = "task"
		primaryClass = stableKind(task.Kind)
		pathFrom = task.ID
	}
	violationClass := classify(evaluation, effect, task)
	path := normalizedPath(view, pathFrom, cancellationID(cancellation))
	result := Signature{
		Version:              Version,
		Contract:             spec.Name,
		ContractVersion:      contractVersion,
		ViolationClass:       violationClass,
		PrimaryEntityKind:    primaryKind,
		PrimaryIdentityClass: primaryClass,
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

func primaryEntity(view evidence.View, evaluation contracts.Result) (*evidence.Effect, *evidence.Task) {
	for index := range view.Effects {
		effect := &view.Effects[index]
		if evaluation.OffendingEffectID != "" && effect.ID == evaluation.OffendingEffectID {
			return effect, nil
		}
	}
	if evaluation.BoundaryOrder > 0 {
		for index := range view.Effects {
			effect := &view.Effects[index]
			if effect.CommitOrder > evaluation.BoundaryOrder || effect.AttemptOrder > evaluation.BoundaryOrder {
				return effect, nil
			}
		}
	}
	for index := range view.Effects {
		if view.Effects[index].State == model.EffectCommitted {
			return &view.Effects[index], nil
		}
	}
	if len(view.Cancellations) > 0 {
		for index := range view.Tasks {
			if view.Tasks[index].ID == view.Cancellations[0].TargetTaskID {
				return nil, &view.Tasks[index]
			}
		}
	}
	if len(view.Tasks) > 0 {
		return nil, &view.Tasks[0]
	}
	return nil, nil
}

func classify(evaluation contracts.Result, effect *evidence.Effect, task *evidence.Task) string {
	if effect != nil && evaluation.BoundaryOrder > 0 {
		if effect.AttemptOrder > evaluation.BoundaryOrder {
			return "post-cancel-effect-attempt"
		}
		if effect.CommitOrder > evaluation.BoundaryOrder {
			return "post-cancel-effect-commit"
		}
	}
	if task != nil && task.TerminalOrder == 0 {
		return "orphan-task"
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
