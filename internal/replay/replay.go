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
	selected := recorded
	if config.Campaign != nil {
		targetDigest, digestErr := config.Campaign.TargetDigest()
		if digestErr != nil {
			return result, digestErr
		}
		if targetDigest != manifest.TargetDigest {
			if !config.AllowTargetMismatch {
				return result, fmt.Errorf("target digest mismatch: capsule=%s current=%s", manifest.TargetDigest, targetDigest)
			}
			result.Comparative = true
		}
		selected = *config.Campaign
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
	result.Observed = append(result.Observed, observed...)
	for _, value := range observed {
		if value.Digest == result.Expected.Digest {
			result.Exact = true
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
