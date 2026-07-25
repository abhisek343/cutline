package contracts

import (
	"fmt"
	"regexp"

	"github.com/abhisek343/cutline/internal/campaign"
	"github.com/abhisek343/cutline/internal/fixtureledger"
	"github.com/abhisek343/cutline/internal/ingest"
	"github.com/abhisek343/cutline/internal/model"
)

var noEffectExpression = regexp.MustCompile(`^builtin\.no_effect_after_cancel_observed\("([A-Za-z0-9._:/-]+)"\)$`)

// EvaluateBuiltins is the milestone-one contract evaluator. It intentionally
// accepts only an explicit built-in selector; arbitrary CEL is invalid until the
// CEL environment is introduced.
func EvaluateBuiltins(snapshot ingest.Snapshot, records []fixtureledger.Record, specs []campaign.ContractSpec) []Result {
	results := make([]Result, 0, len(specs))
	for _, spec := range specs {
		results = append(results, evaluateBuiltin(snapshot, records, spec))
	}
	return results
}

func evaluateBuiltin(snapshot ingest.Snapshot, records []fixtureledger.Record, spec campaign.ContractSpec) Result {
	result := Result{Contract: spec.Name}
	if !snapshot.Complete() {
		result.Status = StatusInconclusive
		result.Message = "evidence is incomplete: " + snapshot.IncompleteReasons[0]
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
	commits := make(map[string]model.Event)
	for _, event := range snapshot.Events {
		if event.Type == model.EventCancelObserved && observedOrder == 0 {
			observedOrder = event.CanonicalOrder
		}
		if event.Type == model.EventEffectCommitted && event.Attributes["kind"] == effectKind {
			commits[event.EntityID] = event
		}
	}
	if observedOrder == 0 {
		result.Status = StatusInconclusive
		result.Message = "required cancellation observation is missing"
		return result
	}

	authoritative := make(map[string]fixtureledger.Record)
	for _, record := range records {
		if record.Kind == effectKind {
			authoritative[record.EffectID] = record
		}
	}
	for effectID, event := range commits {
		if event.CanonicalOrder <= observedOrder {
			continue
		}
		if _, ok := authoritative[effectID]; !ok {
			result.Status = StatusInconclusive
			result.Message = fmt.Sprintf("effect %s claims commitment without authoritative fixture evidence", effectID)
			return result
		}
		result.Status = StatusViolation
		result.Message = fmt.Sprintf("%s committed after cancellation was observed", effectKind)
		result.OffendingEffectID = effectID
		result.BoundaryOrder = observedOrder
		result.EffectOrder = event.CanonicalOrder
		return result
	}

	for effectID := range authoritative {
		if _, ok := commits[effectID]; !ok {
			result.Status = StatusInconclusive
			result.Message = fmt.Sprintf("authoritative effect %s has no matching SDK commitment event", effectID)
			return result
		}
	}
	result.Status = StatusPass
	result.Message = fmt.Sprintf("no authoritative %s effect committed after cancellation observation", effectKind)
	result.BoundaryOrder = observedOrder
	return result
}
