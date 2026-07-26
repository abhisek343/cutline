package explorer

import (
	"fmt"
	"sort"
	"strings"

	"github.com/abhisek343/cutline/internal/model"
)

const defaultPointVisitLimit = 1_000

type Checkpoint struct {
	Name       string `json:"name"`
	FirstOrder uint64 `json:"firstOrder"`
	Visits     int    `json:"visits"`
}

type Discovery struct {
	Checkpoints       []Checkpoint `json:"checkpoints"`
	IncompleteReasons []string     `json:"incompleteReasons,omitempty"`
	Truncated         bool         `json:"truncated"`
}

func (d Discovery) Complete() bool {
	return len(d.Checkpoints) > 0 && len(d.IncompleteReasons) == 0 && !d.Truncated
}

// Discover extracts checkpoint visits from canonical evidence without
// inferring points from timestamps or target logs.
func Discover(events []model.Event, incomplete []string, maxPointVisits int) Discovery {
	if maxPointVisits <= 0 {
		maxPointVisits = defaultPointVisitLimit
	}
	result := Discovery{Checkpoints: []Checkpoint{}}
	result.IncompleteReasons = appendUnique(result.IncompleteReasons, incomplete...)
	ordered := append([]model.Event(nil), events...)
	sort.SliceStable(ordered, func(left, right int) bool {
		return ordered[left].CanonicalOrder < ordered[right].CanonicalOrder
	})
	indices := make(map[string]int)
	totalVisits := 0
	for _, event := range ordered {
		if event.Type != model.EventCheckpointReached {
			continue
		}
		if totalVisits >= maxPointVisits {
			result.Truncated = true
			break
		}
		name := strings.TrimSpace(event.Attributes["point"])
		if name == "" {
			result.IncompleteReasons = appendUnique(result.IncompleteReasons, "checkpoint.reached event has no point name")
			continue
		}
		index, exists := indices[name]
		if !exists {
			indices[name] = len(result.Checkpoints)
			result.Checkpoints = append(result.Checkpoints, Checkpoint{Name: name, FirstOrder: event.CanonicalOrder, Visits: 1})
			totalVisits++
			continue
		}
		result.Checkpoints[index].Visits++
		totalVisits++
	}
	if len(result.Checkpoints) == 0 {
		result.IncompleteReasons = appendUnique(result.IncompleteReasons, "no checkpoints discovered")
	}
	return result
}

type Schedule struct {
	ID            string   `json:"id"`
	Ordinal       int      `json:"ordinal"`
	Strategy      string   `json:"strategy"`
	CancelAt      string   `json:"cancelAt"`
	ReleasePrefix []string `json:"releasePrefix,omitempty"`
	PairPoint     string   `json:"pairPoint,omitempty"`
	PairSide      string   `json:"pairSide,omitempty"`
}

type Plan struct {
	Strategy             string       `json:"strategy"`
	Checkpoints          []Checkpoint `json:"checkpoints"`
	Schedules            []Schedule   `json:"schedules"`
	Unreachable          []string     `json:"unreachable,omitempty"`
	DiscoveryIncomplete  []string     `json:"discoveryIncomplete,omitempty"`
	DiscoveryTruncated   bool         `json:"discoveryTruncated"`
	ScheduleLimitReached bool         `json:"scheduleLimitReached"`
	PrefixDepth          int          `json:"prefixDepth,omitempty"`
}

func (p Plan) Complete() bool {
	return len(p.Schedules) > 0 && len(p.Unreachable) == 0 && len(p.DiscoveryIncomplete) == 0 && !p.DiscoveryTruncated
}

// Enumerate selects the deterministic strategy implementation used by a
// campaign. checkpoint is retained as an alias for the original single-cut
// strategy name.
func Enumerate(strategy string, discovery Discovery, requested []string, maxSchedules, prefixDepth int) (Plan, error) {
	switch strings.TrimSpace(strategy) {
	case "", "checkpoint", "single-cut":
		return EnumerateSingleCut(discovery, requested, maxSchedules)
	case "boundary-pair":
		return EnumerateBoundaryPair(discovery, requested, maxSchedules)
	case "bounded-prefix":
		return EnumerateBoundedPrefix(discovery, requested, maxSchedules, prefixDepth)
	default:
		return Plan{}, fmt.Errorf("unsupported exploration strategy %q", strategy)
	}
}

// EnumerateSingleCut creates one deterministic schedule per discovered point.
// Requested points are a filter; their order cannot override checkpoint
// discovery order. Unknown requested points remain visible to callers.
func EnumerateSingleCut(discovery Discovery, requested []string, maxSchedules int) (Plan, error) {
	plan, selected, err := newPlan("single-cut", discovery, requested, maxSchedules)
	if err != nil {
		return Plan{}, err
	}
	for index, selectedCheckpoint := range selected {
		if index >= maxSchedules {
			plan.ScheduleLimitReached = true
			break
		}
		plan.Schedules = append(plan.Schedules, makeSchedule(plan.Strategy, index+1, selectedCheckpoint.checkpoint.Name, checkpointNames(discovery.Checkpoints[:selectedCheckpoint.index]), "", ""))
	}
	return plan, nil
}

// EnumerateBoundaryPair makes two schedules around each adjacent checkpoint
// boundary: one cancels before the left point is released, and the other
// releases it before cancelling at the right point.
func EnumerateBoundaryPair(discovery Discovery, requested []string, maxSchedules int) (Plan, error) {
	plan, selected, err := newPlan("boundary-pair", discovery, requested, maxSchedules)
	if err != nil {
		return Plan{}, err
	}
	selectedIndexes := make(map[int]struct{}, len(selected))
	for _, checkpoint := range selected {
		selectedIndexes[checkpoint.index] = struct{}{}
	}
	for index := 0; index+1 < len(discovery.Checkpoints); index++ {
		anchor := discovery.Checkpoints[index]
		if _, ok := selectedIndexes[index]; !ok {
			continue
		}
		if len(plan.Schedules) >= maxSchedules {
			plan.ScheduleLimitReached = true
			break
		}
		before := makeSchedule(plan.Strategy, len(plan.Schedules)+1, anchor.Name, checkpointNames(discovery.Checkpoints[:index]), anchor.Name, "before")
		plan.Schedules = append(plan.Schedules, before)
		if len(plan.Schedules) >= maxSchedules {
			plan.ScheduleLimitReached = true
			break
		}
		next := discovery.Checkpoints[index+1]
		after := makeSchedule(plan.Strategy, len(plan.Schedules)+1, next.Name, checkpointNames(discovery.Checkpoints[:index+1]), anchor.Name, "after")
		plan.Schedules = append(plan.Schedules, after)
	}
	return plan, nil
}

// EnumerateBoundedPrefix emits a schedule for each release prefix up to the
// configured depth. Prefix zero cancels at the first discovered point.
func EnumerateBoundedPrefix(discovery Discovery, requested []string, maxSchedules, prefixDepth int) (Plan, error) {
	plan, _, err := newPlan("bounded-prefix", discovery, requested, maxSchedules)
	if err != nil {
		return Plan{}, err
	}
	if prefixDepth <= 0 {
		prefixDepth = 3
	}
	plan.PrefixDepth = prefixDepth
	requestedSet := requestedNames(requested)
	for index, checkpoint := range discovery.Checkpoints {
		if index > prefixDepth {
			break
		}
		if len(requestedSet) > 0 {
			if _, ok := requestedSet[checkpoint.Name]; !ok {
				continue
			}
		}
		if len(plan.Schedules) >= maxSchedules {
			plan.ScheduleLimitReached = true
			break
		}
		plan.Schedules = append(plan.Schedules, makeSchedule(plan.Strategy, len(plan.Schedules)+1, checkpoint.Name, checkpointNames(discovery.Checkpoints[:index]), "", ""))
	}
	return plan, nil
}

type selectedCheckpoint struct {
	checkpoint Checkpoint
	index      int
}

func newPlan(strategy string, discovery Discovery, requested []string, maxSchedules int) (Plan, []selectedCheckpoint, error) {
	if maxSchedules < 1 {
		return Plan{}, nil, fmt.Errorf("max schedules must be positive")
	}
	plan := Plan{
		Strategy:            strategy,
		Checkpoints:         append([]Checkpoint(nil), discovery.Checkpoints...),
		DiscoveryIncomplete: append([]string(nil), discovery.IncompleteReasons...),
		DiscoveryTruncated:  discovery.Truncated,
		Schedules:           []Schedule{},
		Unreachable:         []string{},
	}
	requestedSet := requestedNames(requested)
	seen := make(map[string]struct{}, len(discovery.Checkpoints))
	selected := make([]selectedCheckpoint, 0, len(discovery.Checkpoints))
	for index, checkpoint := range discovery.Checkpoints {
		seen[checkpoint.Name] = struct{}{}
		if len(requestedSet) == 0 {
			selected = append(selected, selectedCheckpoint{checkpoint: checkpoint, index: index})
			continue
		}
		if _, ok := requestedSet[checkpoint.Name]; ok {
			selected = append(selected, selectedCheckpoint{checkpoint: checkpoint, index: index})
		}
	}
	for name := range requestedSet {
		if _, ok := seen[name]; !ok {
			plan.Unreachable = append(plan.Unreachable, name)
		}
	}
	sort.Strings(plan.Unreachable)
	return plan, selected, nil
}

func requestedNames(requested []string) map[string]struct{} {
	result := make(map[string]struct{}, len(requested))
	for _, name := range requested {
		name = strings.TrimSpace(name)
		if name != "" {
			result[name] = struct{}{}
		}
	}
	return result
}

func checkpointNames(checkpoints []Checkpoint) []string {
	result := make([]string, 0, len(checkpoints))
	for _, checkpoint := range checkpoints {
		result = append(result, checkpoint.Name)
	}
	return result
}

func makeSchedule(strategy string, ordinal int, cancelAt string, releasePrefix []string, pairPoint, pairSide string) Schedule {
	parts := []string{strategy, fmt.Sprint(ordinal), cancelAt, strings.Join(releasePrefix, "\x00"), pairPoint, pairSide}
	id, _ := model.ContentID("schedule", parts...)
	return Schedule{ID: id, Ordinal: ordinal, Strategy: strategy, CancelAt: cancelAt, ReleasePrefix: releasePrefix, PairPoint: pairPoint, PairSide: pairSide}
}

func appendUnique(values []string, additions ...string) []string {
	seen := make(map[string]struct{}, len(values)+len(additions))
	for _, value := range values {
		seen[value] = struct{}{}
	}
	for _, value := range additions {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		values = append(values, value)
	}
	return values
}
