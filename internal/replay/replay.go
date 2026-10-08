package replay

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/abhisek343/cutline/internal/campaign"
	"github.com/abhisek343/cutline/internal/capsule"
	"github.com/abhisek343/cutline/internal/explorer"
	"github.com/abhisek343/cutline/internal/signature"
)

const (
	StatusReproduced    = "reproduced"
	StatusNotReproduced = "not-reproduced"
	StatusDifferent     = "different-signature"
	StatusComparative   = "comparative-reproduced"
)

type Execute func(context.Context, campaign.Campaign, explorer.Schedule) ([]signature.Signature, error)

type Config struct {
	BaseDirectory       string
	AdapterVersion      string
	Campaign            *campaign.Campaign
	AllowTargetMismatch bool
	CheckPrerequisites  func([]string) error
}

type Result struct {
	Status      string                `json:"status"`
	Exact       bool                  `json:"exact"`
	Comparative bool                  `json:"comparative"`
	Manifest    capsule.Manifest      `json:"manifest"`
	Campaign    campaign.Campaign     `json:"campaign"`
	Schedule    explorer.Schedule     `json:"schedule"`
	Expected    signature.Signature   `json:"expected"`
	Observed    []signature.Signature `json:"observed"`
}

func Run(ctx context.Context, root string, config Config, execute Execute) (Result, error) {
	if execute == nil {
		return Result{}, fmt.Errorf("replay executor is required")
	}
	manifest, err := capsule.ValidateDirectory(root)
	if err != nil {
		return Result{}, fmt.Errorf("validate capsule: %w", err)
	}
	result := Result{Manifest: manifest, Expected: manifest.FailureSignature, Observed: []signature.Signature{}}
	if config.CheckPrerequisites != nil {
		if err := config.CheckPrerequisites(manifest.ReplayPrerequisites); err != nil {
			return result, fmt.Errorf("replay prerequisites: %w", err)
		}
	}
	campaignData, err := capsule.ReadArtifact(root, "campaign.yaml")
	if err != nil {
		return result, err
	}
	recorded, err := campaign.Parse(campaignData)
	if err != nil {
		return result, fmt.Errorf("parse capsule campaign: %w", err)
	}
	recordedDigest, err := recorded.Digest()
	if err != nil {
		return result, err
	}
	if recordedDigest != manifest.CampaignDigest {
		return result, fmt.Errorf("capsule campaign digest does not match manifest")
	}
	if recorded.Target.Adapter != manifest.Adapter {
		return result, fmt.Errorf("capsule campaign adapter does not match manifest")
	}
	selected := recorded
	if config.Campaign != nil {
		selected = *config.Campaign
	}
	currentVersion := config.AdapterVersion
	if currentVersion == "" {
		switch selected.Target.Adapter {
		case "go-test", "go-command":
			currentVersion = "native/" + selected.Target.Adapter + "/1"
		default:
			return result, fmt.Errorf("replay requires the current adapter version for %q", selected.Target.Adapter)
		}
	}
	selectedDigest, err := selected.Digest()
	if err != nil {
		return result, err
	}
	targetDigest, err := selected.ExecutionDigest(ctx, config.BaseDirectory)
	if err != nil {
		return result, fmt.Errorf("establish replay target identity: %w", err)
	}
	mismatches := []string{}
	if targetDigest != manifest.TargetDigest {
		mismatches = append(mismatches, "target build digest mismatch")
	}
	if selectedDigest != manifest.CampaignDigest {
		mismatches = append(mismatches, "campaign digest mismatch")
	}
	if currentVersion != manifest.AdapterVersion || selected.Target.Adapter != manifest.Adapter {
		mismatches = append(mismatches, "adapter version mismatch")
	}
	if len(mismatches) > 0 {
		if !config.AllowTargetMismatch {
			return result, fmt.Errorf("incompatible replay: %s; use explicit comparative replay for intentional changes", strings.Join(mismatches, "; "))
		}
		result.Comparative = true
	}
	scheduleData, err := capsule.ReadArtifact(root, "schedule.json")
	if err != nil {
		return result, err
	}
	if err := json.Unmarshal(scheduleData, &result.Schedule); err != nil {
		return result, fmt.Errorf("decode capsule schedule: %w", err)
	}
	result.Campaign = selected
	observed, err := execute(ctx, selected, result.Schedule)
	if err != nil {
		return result, fmt.Errorf("execute replay: %w", err)
	}
	afterDigest, err := selected.ExecutionDigest(ctx, config.BaseDirectory)
	if err != nil {
		return result, fmt.Errorf("verify replay target identity: %w", err)
	}
	if afterDigest != targetDigest {
		return result, fmt.Errorf("target build changed during replay")
	}
	result.Observed = append(result.Observed, observed...)
	for _, value := range observed {
		if value.Digest == result.Expected.Digest {
			result.Exact = !result.Comparative
			if result.Comparative {
				result.Status = StatusComparative
			} else {
				result.Status = StatusReproduced
			}
			return result, nil
		}
	}
	result.Status = StatusNotReproduced
	if len(observed) > 0 {
		result.Status = StatusDifferent
	}
	return result, nil
}

func Summary(result Result) string {
	if result.Comparative {
		return fmt.Sprintf("%s (%s)", result.Status, strings.TrimPrefix(result.Expected.Digest, "sha256:"))
	}
	return result.Status
}
