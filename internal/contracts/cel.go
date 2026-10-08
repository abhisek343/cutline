package contracts

import (
	"fmt"
	"reflect"
	"sort"
	"time"

	"github.com/abhisek343/cutline/internal/campaign"
	"github.com/abhisek343/cutline/internal/evidence"
	"github.com/abhisek343/cutline/internal/model"
	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
)

const (
	EnvironmentVersion = 1
	DefaultCostLimit   = uint64(50_000)
)

// CELEngine is immutable after construction. A frozen evidence view is the
// only input to Evaluate; helpers have no filesystem, network, or clock access.
type CELEngine struct {
	env       *cel.Env
	costLimit uint64
}

func NewCELEngine(costLimit uint64) (*CELEngine, error) {
	if costLimit == 0 {
		costLimit = DefaultCostLimit
	}
	env, err := cel.NewEnv(
		cel.EnableMacroCallTracking(),
		cel.Variable("effects", cel.ListType(cel.DynType)),
		cel.Variable("tasks", cel.ListType(cel.DynType)),
		cel.Variable("resources", cel.ListType(cel.DynType)),
		cel.Variable("cancel", cel.DynType),
		cel.Variable("run", cel.DynType),
		cel.Function("startedAfter", cel.MemberOverload("cutline_started_after_dyn_dyn", []*cel.Type{cel.DynType, cel.DynType}, cel.BoolType, cel.BinaryBinding(startedAfter))),
		cel.Function("descendsFrom", cel.MemberOverload("cutline_descends_from_dyn_dyn", []*cel.Type{cel.DynType, cel.DynType}, cel.BoolType, cel.BinaryBinding(descendsFrom))),
		cel.Function("isTerminalAt", cel.MemberOverload("cutline_terminal_at_dyn_dyn", []*cel.Type{cel.DynType, cel.DynType}, cel.BoolType, cel.BinaryBinding(isTerminalAt))),
		cel.Function("wasCompensatedBefore", cel.MemberOverload("cutline_compensated_before_dyn_dyn", []*cel.Type{cel.DynType, cel.DynType}, cel.BoolType, cel.BinaryBinding(wasCompensatedBefore))),
		cel.Function("isReleasedAt", cel.MemberOverload("cutline_released_at_dyn_dyn", []*cel.Type{cel.DynType, cel.DynType}, cel.BoolType, cel.BinaryBinding(isReleasedAt))),
		cel.Function("expiredSafely", cel.MemberOverload("cutline_expired_safely_dyn", []*cel.Type{cel.DynType}, cel.BoolType, cel.UnaryBinding(expiredSafely))),
	)
	if err != nil {
		return nil, fmt.Errorf("create CEL environment v%d: %w", EnvironmentVersion, err)
	}
	return &CELEngine{env: env, costLimit: costLimit}, nil
}

// Evaluate dispatches the legacy built-in spelling and the versioned CEL
// environment. Keeping the built-in path makes old capsules readable while
// new campaigns can use ordinary CEL expressions.
func Evaluate(view evidence.View, specs []campaign.ContractSpec) []Result {
	engine, err := NewCELEngine(DefaultCostLimit)
	results := make([]Result, 0, len(specs))
	for _, spec := range specs {
		if err != nil {
			results = append(results, Result{Contract: spec.Name, Status: StatusInvalid, Message: err.Error()})
			continue
		}
		if noEffectExpression.MatchString(spec.Expression) {
			results = append(results, evaluateBuiltin(view, spec))
			continue
		}
		results = append(results, engine.Evaluate(view, spec))
	}
	return results
}

func (e *CELEngine) Evaluate(view evidence.View, spec campaign.ContractSpec) Result {
	result := Result{Contract: spec.Name}
	version := spec.Version
	if version == 0 {
		version = 1
	}
	if version < 1 || version > EnvironmentVersion {
		result.Status = StatusInvalid
		result.Message = fmt.Sprintf("contract version %d is unsupported by CEL environment v%d", version, EnvironmentVersion)
		return result
	}
	if !view.Complete() {
		return inconclusive(view, result)
	}
	if unresolvedEffect(view) {
		result.Status, result.Message = StatusInconclusive, "effect dependency outcome is unknown"
		return result
	}
	for _, required := range spec.Requires {
		if !view.HasCapability(required) {
			result.Status = StatusInconclusive
			result.Message = "required capability is missing: " + required
			return result
		}
	}
	if spec.Boundary != "" {
		order, ok := view.BoundaryOrder(spec.Boundary)
		if !ok {
			result.Status = StatusInconclusive
			result.Message = "required boundary is missing: " + spec.Boundary
			return result
		}
		result.BoundaryOrder = order
	}
	ast, issues := e.env.Compile(spec.Expression)
	if issues.Err() != nil {
		result.Status = StatusInvalid
		result.Message = "CEL compile error: " + issues.Err().Error()
		return result
	}
	program, err := e.env.Program(ast, cel.CostLimit(e.costLimit), cel.InterruptCheckFrequency(100))
	if err != nil {
		result.Status = StatusInvalid
		result.Message = "CEL program error: " + err.Error()
		return result
	}
	value, _, err := program.Eval(activation(view))
	if err != nil {
		result.Status = StatusInconclusive
		result.Message = "CEL evaluation did not complete: " + err.Error()
		return result
	}
	native, err := value.ConvertToNative(reflect.TypeOf(true))
	if err != nil {
		result.Status = StatusInvalid
		result.Message = "CEL expression must return bool: " + err.Error()
		return result
	}
	passed, ok := native.(bool)
	if !ok {
		result.Status = StatusInvalid
		result.Message = "CEL expression must return bool"
		return result
	}
	if passed {
		result.Status, result.Message = StatusPass, "CEL contract passed"
	} else {
		result.Status, result.Message = StatusViolation, "CEL contract evaluated to false"
		if id := e.effectWitness(view, ast); id != "" {
			result.OffendingEffectID = id
			for _, effect := range view.Effects {
				if effect.ID == id {
					result.EffectOrder = effect.CommitOrder
					if effect.IdempotencyKey == "" {
						result.Message += "; minimization unavailable: effect has no stable idempotency key"
					}
				}
			}
		} else {
			result.Message += "; minimization unavailable: expression has no supported effect witness"
		}
	}
	return result
}

// effectWitness supports exactly !effects.exists(e, predicate). The predicate
// is evaluated against the original full activation, not a filtered evidence
// set: nested quantifiers and aggregate references retain their meaning.
// General CEL expressions do not necessarily have an entity counterexample.
func (e *CELEngine) effectWitness(view evidence.View, ast *cel.Ast) string {
	root := ast.Expr().GetCallExpr()
	if root == nil || root.Function != "!_" || len(root.Args) != 1 {
		return ""
	}
	macro := ast.SourceInfo().GetMacroCalls()[root.Args[0].Id].GetCallExpr()
	if macro == nil || macro.Function != "exists" || macro.Target.GetIdentExpr().GetName() != "effects" || len(macro.Args) != 2 {
		return ""
	}
	name := macro.Args[0].GetIdentExpr().GetName()
	if name == "" {
		return ""
	}
	env, err := e.env.Extend(cel.Variable(name, cel.DynType))
	if err != nil {
		return ""
	}
	parsed, err := cel.AstToParsedExpr(ast)
	if err != nil {
		return ""
	}
	parsed.Expr = macro.Args[1]
	checked, issues := env.Check(cel.ParsedExprToAst(parsed))
	if issues.Err() != nil {
		return ""
	}
	program, err := env.Program(checked, cel.CostTracking(nil), cel.CostLimit(e.costLimit), cel.InterruptCheckFrequency(100))
	if err != nil {
		return ""
	}
	values := activation(view)
	candidates := make([]int, len(view.Effects))
	for index := range candidates {
		candidates[index] = index
	}
	sort.Slice(candidates, func(i, j int) bool {
		left, right := view.Effects[candidates[i]], view.Effects[candidates[j]]
		if left.Kind != right.Kind {
			return left.Kind < right.Kind
		}
		if left.IdempotencyKey != right.IdempotencyKey {
			return left.IdempotencyKey < right.IdempotencyKey
		}
		return left.EvidenceSource < right.EvidenceSource
	})
	remaining := e.costLimit
	for _, index := range candidates {
		values[name] = values["effects"].([]any)[index]
		value, details, err := program.Eval(values)
		cost := uint64(1)
		if details != nil && details.ActualCost() != nil && *details.ActualCost() > cost {
			cost = *details.ActualCost()
		}
		if cost > remaining {
			return ""
		}
		remaining -= cost
		if err == nil && value == types.True {
			return view.Effects[index].ID
		}
		if remaining == 0 {
			return ""
		}
	}
	return ""
}

func unresolvedEffect(view evidence.View) bool {
	for _, effect := range view.Effects {
		if effect.State == model.EffectUnknown || effect.State == model.EffectAttempted {
			return true
		}
	}
	return false
}

func inconclusive(view evidence.View, result Result) Result {
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

func activation(view evidence.View) map[string]any {
	cancel := map[string]any{}
	if len(view.Cancellations) > 0 {
		c := view.Cancellations[0]
		cancel["id"], cancel["targetTask"] = c.ID, c.TargetTaskID
		cancel["requestedAt"] = eventTime(view, c.ID, "requested")
		cancel["deliveredAt"] = eventTime(view, c.ID, "delivered")
		cancel["observedAt"] = eventTime(view, c.ID, "observed")
	}
	run := map[string]any{"drainedAt": time.Time{}}
	if at, ok := view.BoundaryTime("run-drained"); ok {
		run["drainedAt"] = at
	}
	effects := make([]any, 0, len(view.Effects))
	for _, effect := range view.Effects {
		effects = append(effects, map[string]any{
			"id": effect.ID, "kind": effect.Kind, "committed": effect.CommitOrder > 0,
			"startedAt": eventTime(view, effect.ID, "attempted"), "committedAt": eventTime(view, effect.ID, "committed"),
			"ownerTask": effect.OwnerTaskID, "idempotencyKey": effect.IdempotencyKey,
			"compensated": effect.State == model.EffectCompensated, "compensatedAt": eventTime(view, effect.ID, "compensated"), "terminal": effect.TerminalOrder > 0,
		})
	}
	tasks := make([]any, 0, len(view.Tasks))
	for _, task := range view.Tasks {
		ancestors := make([]string, 0)
		for current := task.ParentID; current != ""; {
			ancestors = append(ancestors, current)
			current = taskParent(view, current)
		}
		tasks = append(tasks, map[string]any{"id": task.ID, "parentId": task.ParentID, "state": string(task.State), "terminal": task.TerminalOrder > 0, "ancestors": ancestors, "terminalAt": eventTime(view, task.ID, "finished")})
	}
	resources := make([]any, 0, len(view.Resources))
	for _, resource := range view.Resources {
		resources = append(resources, map[string]any{"id": resource.ID, "ownerTask": resource.OwnerTaskID, "released": resource.State == model.ResourceReleased, "expired": resource.State == model.ResourceExpired, "releasedAt": eventTime(view, resource.ID, "released")})
	}
	return map[string]any{"effects": effects, "tasks": tasks, "resources": resources, "cancel": cancel, "run": run}
}

func eventTime(view evidence.View, entityID, phase string) time.Time {
	wanted := map[string]model.EventType{"requested": model.EventCancelRequested, "delivered": model.EventCancelDelivered, "observed": model.EventCancelObserved, "attempted": model.EventEffectAttempted, "committed": model.EventEffectCommitted, "compensated": model.EventEffectCompensated, "finished": model.EventTaskFinished, "released": model.EventResourceReleased}[phase]
	for _, event := range view.Events {
		if event.EntityID == entityID && event.Type == wanted {
			return event.ObservedAt
		}
	}
	return time.Time{}
}

func taskParent(view evidence.View, id string) string {
	for _, task := range view.Tasks {
		if task.ID == id {
			return task.ParentID
		}
	}
	return ""
}

func mapNative(value ref.Val) map[string]any {
	native, err := value.ConvertToNative(reflect.TypeOf(map[string]any{}))
	if err != nil {
		return nil
	}
	result, _ := native.(map[string]any)
	return result
}

func timestampNative(value ref.Val) (time.Time, bool) {
	native, err := value.ConvertToNative(reflect.TypeOf(time.Time{}))
	if err != nil {
		return time.Time{}, false
	}
	result, ok := native.(time.Time)
	return result, ok
}

func startedAfter(effect, boundary ref.Val) ref.Val {
	values := mapNative(effect)
	when, ok := timestampNative(boundary)
	if values == nil || !ok || when.IsZero() {
		return types.NewErr("startedAfter requires a recorded boundary timestamp")
	}
	at, ok := values["startedAt"].(time.Time)
	if !ok || at.IsZero() {
		return types.NewErr("startedAfter requires a recorded effect attempt timestamp")
	}
	return types.Bool(at.After(when))
}

func descendsFrom(task, ancestor ref.Val) ref.Val {
	values := mapNative(task)
	if values == nil {
		return types.Bool(false)
	}
	want, ok := ancestor.Value().(string)
	if !ok {
		return types.Bool(false)
	}
	ancestors, _ := values["ancestors"].([]string)
	for _, value := range ancestors {
		if value == want {
			return types.Bool(true)
		}
	}
	return types.Bool(false)
}

// "At" is inclusive; "Before" is strict. A recorded lifecycle outcome
// without its timestamp, or a missing boundary, is unknown rather than false.
func lifecycleAt(entity, boundary ref.Val, state, phase string, inclusive bool) ref.Val {
	values := mapNative(entity)
	when, ok := timestampNative(boundary)
	if values == nil || !ok || when.IsZero() {
		return types.NewErr("%s requires a recorded boundary timestamp", phase)
	}
	if values[state] != true {
		return types.False
	}
	at, ok := values[phase].(time.Time)
	if !ok || at.IsZero() {
		return types.NewErr("%s requires a recorded lifecycle timestamp", phase)
	}
	return types.Bool(at.Before(when) || (inclusive && at.Equal(when)))
}

func isTerminalAt(task, boundary ref.Val) ref.Val {
	return lifecycleAt(task, boundary, "terminal", "terminalAt", true)
}

func wasCompensatedBefore(effect, boundary ref.Val) ref.Val {
	return lifecycleAt(effect, boundary, "compensated", "compensatedAt", false)
}

func isReleasedAt(resource, boundary ref.Val) ref.Val {
	return lifecycleAt(resource, boundary, "released", "releasedAt", true)
}

func expiredSafely(resource ref.Val) ref.Val {
	values := mapNative(resource)
	return types.Bool(values != nil && values["expired"] == true)
}
