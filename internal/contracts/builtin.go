package contracts

import (
	"fmt"
	"regexp"

	"github.com/abhisek343/cutline/internal/campaign"
	"github.com/abhisek343/cutline/internal/evidence"
	"github.com/abhisek343/cutline/internal/model"
)

var noEffectExpression = regexp.MustCompile(`^builtin\.no_effect_after_cancel_observed\("([A-Za-z0-9._:/-]+)"\)$`)

// EvaluateBuiltins is retained as a small, typed compatibility evaluator while
// the full CEL engine is introduced in slice 14.
func EvaluateBuiltins(view evidence.View, specs []campaign.ContractSpec) []Result {
	results := make([]Result, 0, len(specs))
	for _, spec := range specs {
		results = append(results, evaluateBuiltin(view, spec))
	}
	return results
}

func evaluateBuiltin(view evidence.View, spec campaign.ContractSpec) Result {
	result := Result{Contract: spec.Name}
	if !view.Complete() {
		result.Status = StatusInconclusive
		if len(view.Issues) > 0 {
			result.Message = "evidence is inconsistent: " + view.Issues[0].Message
		} else if len(view.IncompleteReasons) > 0 {
			result.Message = "evidence is incomplete: " + view.IncompleteReasons[0]
		} else {
			result.Message = "evidence is incomplete"
		}
		return result
	}
	match := noEffectExpression.FindStringSubmatch(spec.Expression)
	if match == nil {
		result.Status = StatusInvalid
		result.Message = "CEL contracts are not available yet and the expression is not a supported built-in"
		return result
	}
	effectKind := match[1]

	var observedOrder uint64
	for _, cancellation := range view.Cancellations {
		if cancellation.ObservedOrder > 0 && (observedOrder == 0 || cancellation.ObservedOrder < observedOrder) {
			observedOrder = cancellation.ObservedOrder
		}
	}
	if observedOrder == 0 {
		result.Status = StatusInconclusive
		result.Message = "required cancellation observation is missing"
		return result
	}

	for _, effect := range view.Effects {
		if effect.Kind != effectKind || effect.State != model.EffectCommitted {
			continue
		}
		if effect.CommitOrder > observedOrder {
			result.Status = StatusViolation
			result.Message = fmt.Sprintf("%s committed after cancellation was observed", effectKind)
			result.OffendingEffectID = effect.ID
			result.BoundaryOrder = observedOrder
			result.EffectOrder = effect.CommitOrder
			return result
		}
	}
	result.Status = StatusPass
	result.Message = fmt.Sprintf("no authoritative %s effect committed after cancellation observation", effectKind)
	result.BoundaryOrder = observedOrder
	return result
}
