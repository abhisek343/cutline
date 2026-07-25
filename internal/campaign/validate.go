package campaign

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

var (
	supportedAdapters   = []string{"go-test", "go-command", "temporal"}
	supportedStrategies = []string{"checkpoint", "single-cut", "boundary-pair", "bounded-prefix"}
	supportedSeverities = []string{"critical", "high", "medium", "low"}
	supportedBoundaries = []string{
		"",
		"cancellation-requested",
		"cancellation-delivered",
		"cancellation-observed",
		"target-returned",
		"run-drained",
	}
)

func applyDefaults(c *Campaign) {
	if c.Exploration.Strategy == "" {
		c.Exploration.Strategy = "checkpoint"
	}
	if c.Exploration.MaxSchedules == 0 {
		c.Exploration.MaxSchedules = 200
	}
	if c.Exploration.MaxPointVisits == 0 {
		c.Exploration.MaxPointVisits = 1_000
	}
	if c.Exploration.ScheduleTimeout == 0 {
		c.Exploration.ScheduleTimeout = Duration(60 * time.Second)
	}
	if c.Exploration.DrainTimeout == 0 {
		c.Exploration.DrainTimeout = Duration(10 * time.Second)
	}
	if c.Output.Directory == "" {
		c.Output.Directory = ".cutline"
	}
	if c.Output.Format == "" {
		c.Output.Format = "text"
	}
	for i := range c.Contracts {
		if c.Contracts[i].Version == 0 {
			c.Contracts[i].Version = 1
		}
	}
}

// Validate checks semantic constraints after strict decoding and defaulting.
func (c Campaign) Validate() error {
	if c.APIVersion != APIVersionV1Alpha1 {
		return fieldError(ErrUnsupportedVersion, "apiVersion", fmt.Sprintf("must equal %q", APIVersionV1Alpha1))
	}
	if c.Kind != KindCampaign {
		return fieldError(ErrInvalidCampaign, "kind", fmt.Sprintf("must equal %q", KindCampaign))
	}
	if strings.TrimSpace(c.Name) == "" {
		return fieldError(ErrInvalidCampaign, "name", "must not be blank")
	}
	if !slices.Contains(supportedAdapters, c.Target.Adapter) {
		return fieldError(ErrInvalidCampaign, "target.adapter", "is unsupported")
	}
	if len(c.Target.Command) == 0 {
		return fieldError(ErrInvalidCampaign, "target.command", "must contain an executable")
	}
	for i, arg := range c.Target.Command {
		if strings.ContainsRune(arg, '\x00') {
			return fieldError(ErrInvalidCampaign, fmt.Sprintf("target.command[%d]", i), "must not contain NUL")
		}
	}
	if c.Target.WorkingDirectory != "" && filepath.IsAbs(c.Target.WorkingDirectory) {
		return fieldError(ErrInvalidCampaign, "target.workingDirectory", "must be relative to the campaign")
	}
	if !slices.Contains(supportedStrategies, c.Exploration.Strategy) {
		return fieldError(ErrInvalidCampaign, "exploration.strategy", "is unsupported")
	}
	if c.Exploration.MaxSchedules < 1 || c.Exploration.MaxSchedules > 100_000 {
		return fieldError(ErrInvalidCampaign, "exploration.maxSchedules", "must be between 1 and 100000")
	}
	if c.Exploration.MaxPointVisits < 1 || c.Exploration.MaxPointVisits > 1_000_000 {
		return fieldError(ErrInvalidCampaign, "exploration.maxPointVisits", "must be between 1 and 1000000")
	}
	if c.Exploration.ScheduleTimeout.Value() <= 0 {
		return fieldError(ErrInvalidCampaign, "exploration.scheduleTimeout", "must be positive")
	}
	if c.Exploration.DrainTimeout.Value() <= 0 {
		return fieldError(ErrInvalidCampaign, "exploration.drainTimeout", "must be positive")
	}
	if len(c.Contracts) == 0 {
		return fieldError(ErrInvalidCampaign, "contracts", "must contain at least one contract")
	}

	seenContracts := make(map[string]struct{}, len(c.Contracts))
	for i, contract := range c.Contracts {
		path := fmt.Sprintf("contracts[%d]", i)
		name := strings.TrimSpace(contract.Name)
		if name == "" {
			return fieldError(ErrInvalidCampaign, path+".name", "must not be blank")
		}
		if _, exists := seenContracts[name]; exists {
			return fieldError(ErrInvalidCampaign, path+".name", "must be unique")
		}
		seenContracts[name] = struct{}{}
		if contract.Version < 1 {
			return fieldError(ErrInvalidCampaign, path+".version", "must be positive")
		}
		if !slices.Contains(supportedSeverities, contract.Severity) {
			return fieldError(ErrInvalidCampaign, path+".severity", "is unsupported")
		}
		if !slices.Contains(supportedBoundaries, contract.Boundary) {
			return fieldError(ErrInvalidCampaign, path+".boundary", "is unsupported")
		}
		if strings.TrimSpace(contract.Expression) == "" {
			return fieldError(ErrInvalidCampaign, path+".expression", "must not be blank")
		}
		for j, capability := range contract.Requires {
			if strings.TrimSpace(capability) == "" {
				return fieldError(ErrInvalidCampaign, fmt.Sprintf("%s.requires[%d]", path, j), "must not be blank")
			}
		}
	}
	if filepath.IsAbs(c.Output.Directory) {
		return fieldError(ErrInvalidCampaign, "output.directory", "must be relative")
	}
	if c.Output.Format != "text" && c.Output.Format != "json" && c.Output.Format != "html" {
		return fieldError(ErrInvalidCampaign, "output.format", "must be text, json, or html")
	}
	return nil
}

// IsInvalid reports whether an error is a campaign validation category.
func IsInvalid(err error) bool {
	return errors.Is(err, ErrInvalidCampaign) || errors.Is(err, ErrUnsupportedVersion)
}
