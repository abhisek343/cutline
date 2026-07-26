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
	ID       string `json:"id"`
	Ordinal  int    `json:"ordinal"`
	Strategy string `json:"strategy"`
	CancelAt string `json:"cancelAt"`
}

type Plan struct {
	Strategy             string       `json:"strategy"`
	Checkpoints          []Checkpoint `json:"checkpoints"`
	Schedules            []Schedule   `json:"schedules"`
	Unreachable          []string     `json:"unreachable,omitempty"`
	DiscoveryIncomplete  []string     `json:"discoveryIncomplete,omitempty"`
	DiscoveryTruncated   bool         `json:"discoveryTruncated"`
	ScheduleLimitReached bool         `json:"scheduleLimitReached"`
}

func (p Plan) Complete() bool {
	return len(p.Schedules) > 0 && len(p.Unreachable) == 0 && len(p.DiscoveryIncomplete) == 0 && !p.DiscoveryTruncated
}

// EnumerateSingleCut creates one deterministic schedule per discovered point.
// Requested points are a filter; their order cannot override checkpoint
// discovery order. Unknown requested points remain visible to callers.
func EnumerateSingleCut(discovery Discovery, requested []string, maxSchedules int) (Plan, error) {
	if maxSchedules < 1 {
		return Plan{}, fmt.Errorf("max schedules must be positive")
	}
	plan := Plan{
		Strategy:            "single-cut",
		Checkpoints:         append([]Checkpoint(nil), discovery.Checkpoints...),
		DiscoveryIncomplete: append([]string(nil), discovery.IncompleteReasons...),
		DiscoveryTruncated:  discovery.Truncated,
		Schedules:           []Schedule{},
		Unreachable:         []string{},
	}
	requestedSet := make(map[string]struct{}, len(requested))
	for _, name := range requested {
		name = strings.TrimSpace(name)
		if name != "" {
			requestedSet[name] = struct{}{}
		}
	}
	seen := make(map[string]struct{}, len(discovery.Checkpoints))
	selected := make([]Checkpoint, 0, len(discovery.Checkpoints))
	for _, checkpoint := range discovery.Checkpoints {
		seen[checkpoint.Name] = struct{}{}
		if len(requestedSet) == 0 {
			selected = append(selected, checkpoint)
			continue
		}
		if _, ok := requestedSet[checkpoint.Name]; ok {
			selected = append(selected, checkpoint)
		}
	}
	for name := range requestedSet {
		if _, ok := seen[name]; !ok {
			plan.Unreachable = append(plan.Unreachable, name)
		}
	}
	sort.Strings(plan.Unreachable)
	for index, checkpoint := range selected {
		if index >= maxSchedules {
			plan.ScheduleLimitReached = true
			break
		}
		id, err := model.ContentID("schedule", plan.Strategy, fmt.Sprint(index+1), checkpoint.Name)
		if err != nil {
			return Plan{}, fmt.Errorf("create schedule identity: %w", err)
		}
		plan.Schedules = append(plan.Schedules, Schedule{ID: id, Ordinal: index + 1, Strategy: plan.Strategy, CancelAt: checkpoint.Name})
	}
	return plan, nil
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
