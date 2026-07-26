package minimize

import (
	"context"
	"fmt"
	"math"

	"github.com/abhisek343/cutline/internal/explorer"
	"github.com/abhisek343/cutline/internal/signature"
)

type Execute func(context.Context, explorer.Schedule) ([]signature.Signature, error)

type Config struct {
	MaxAttempts   int
	Confirmations int
}

type Attempt struct {
	Candidate       explorer.Schedule `json:"candidate"`
	Reproduced      bool              `json:"reproduced"`
	SignatureDigest string            `json:"signatureDigest,omitempty"`
	Error           string            `json:"error,omitempty"`
}

type Result struct {
	Original        explorer.Schedule   `json:"original"`
	Minimized       explorer.Schedule   `json:"minimized"`
	Target          signature.Signature `json:"target"`
	Attempts        []Attempt           `json:"attempts"`
	Stable          bool                `json:"stable"`
	BudgetExhausted bool                `json:"budgetExhausted"`
}

func Minimize(ctx context.Context, original explorer.Schedule, target signature.Signature, config Config, execute Execute) (Result, error) {
	if target.Digest == "" {
		return Result{}, fmt.Errorf("target failure signature is required")
	}
	if execute == nil {
		return Result{}, fmt.Errorf("candidate executor is required")
	}
	if config.MaxAttempts <= 0 {
		config.MaxAttempts = 100
	}
	if config.Confirmations <= 0 {
		config.Confirmations = 3
	}
	result := Result{Original: original, Minimized: original, Target: target, Attempts: []Attempt{}}
	prefix := append([]string(nil), original.ReleasePrefix...)
	granularity := 2
	for len(prefix) > 0 && len(result.Attempts) < config.MaxAttempts {
		if granularity > len(prefix) {
			granularity = len(prefix)
		}
		chunkSize := int(math.Ceil(float64(len(prefix)) / float64(granularity)))
		reduced := false
		for start := 0; start < len(prefix) && len(result.Attempts) < config.MaxAttempts; start += chunkSize {
			end := start + chunkSize
			if end > len(prefix) {
				end = len(prefix)
			}
			candidatePrefix := append([]string(nil), prefix[:start]...)
			candidatePrefix = append(candidatePrefix, prefix[end:]...)
			candidate := original.WithReleasePrefix(candidatePrefix)
			reproduced, attempt, err := run(ctx, candidate, target, execute)
			result.Attempts = append(result.Attempts, attempt)
			if err != nil {
				return result, err
			}
			if reproduced {
				prefix = candidatePrefix
				result.Minimized = candidate
				granularity = maxInt(2, granularity-1)
				reduced = true
				break
			}
		}
		if reduced {
			continue
		}
		if granularity >= len(prefix) {
			break
		}
		granularity = minInt(len(prefix), granularity*2)
	}
	if len(result.Attempts) >= config.MaxAttempts {
		result.BudgetExhausted = true
	}
	result.Minimized = original.WithReleasePrefix(prefix)
	result.Stable = true
	for confirmation := 0; confirmation < config.Confirmations; confirmation++ {
		if len(result.Attempts) >= config.MaxAttempts {
			result.BudgetExhausted = true
			result.Stable = false
			break
		}
		reproduced, attempt, err := run(ctx, result.Minimized, target, execute)
		result.Attempts = append(result.Attempts, attempt)
		if err != nil {
			return result, err
		}
		if !reproduced {
			result.Stable = false
		}
	}
	return result, nil
}

func run(ctx context.Context, candidate explorer.Schedule, target signature.Signature, execute Execute) (bool, Attempt, error) {
	if err := ctx.Err(); err != nil {
		return false, Attempt{Candidate: candidate, Error: err.Error()}, err
	}
	values, err := execute(ctx, candidate)
	attempt := Attempt{Candidate: candidate}
	if err != nil {
		attempt.Error = err.Error()
		return false, attempt, fmt.Errorf("execute minimization candidate %s: %w", candidate.ID, err)
	}
	for _, value := range values {
		if attempt.SignatureDigest == "" {
			attempt.SignatureDigest = value.Digest
		}
		if value.Digest == target.Digest {
			attempt.Reproduced = true
			attempt.SignatureDigest = value.Digest
			break
		}
	}
	return attempt.Reproduced, attempt, nil
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}
