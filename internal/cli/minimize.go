package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/abhisek343/cutline/internal/campaign"
	"github.com/abhisek343/cutline/internal/explorer"
	"github.com/abhisek343/cutline/internal/minimize"
	"github.com/abhisek343/cutline/internal/signature"
	"github.com/spf13/cobra"
)

type minimizeOutput struct {
	Campaign     string              `json:"campaign"`
	Schedule     explorer.Schedule   `json:"schedule"`
	Signature    signature.Signature `json:"signature"`
	Minimization minimize.Result     `json:"minimization"`
	Capsule      string              `json:"capsule"`
}

// newMinimizeCommand finds a real failing native schedule, minimizes only
// candidates that reproduce its exact signature, and writes one new capsule.
func newMinimizeCommand() *cobra.Command {
	var campaignPath, outputDirectory, requestedDigest string
	var outputJSON bool
	var maxAttempts, confirmations int
	command := &cobra.Command{
		Use:   "minimize --campaign <campaign.yaml> --output <new-capsule-directory>",
		Short: "minimize one failing schedule and write a replay capsule",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if maxAttempts < 1 || confirmations < 1 {
				return fmt.Errorf("max-attempts and confirmations must be positive")
			}
			spec, err := campaign.LoadFile(campaignPath)
			if err != nil {
				return err
			}
			baseDirectory, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("resolve current working directory: %w", err)
			}
			destination, err := localNewDirectory(baseDirectory, outputDirectory)
			if err != nil {
				return err
			}
			runner, err := runnerForCampaign(spec, baseDirectory)
			if err != nil {
				return err
			}
			_, discovery, err := runner.Discover(cmd.Context(), spec)
			if err != nil {
				return err
			}
			plan, err := explorer.Enumerate(spec.Exploration.Strategy, discovery, spec.Exploration.CancelAt, spec.Exploration.MaxSchedules, spec.Exploration.PrefixDepth)
			if err != nil {
				return err
			}
			unsignedViolation := false
			for _, schedule := range plan.Schedules {
				result, runErr := runner.RunSchedule(cmd.Context(), spec, schedule)
				if runErr != nil {
					return fmt.Errorf("run schedule %s: %w", schedule.ID, runErr)
				}
				target, found := selectSignature(result.Signatures, requestedDigest)
				if !found {
					if len(result.Signatures) == 0 && result.Status == "violation" {
						unsignedViolation = true
					}
					continue
				}
				minimized, minErr := runner.MinimizeSchedule(cmd.Context(), spec, schedule, target, minimize.Config{MaxAttempts: maxAttempts, Confirmations: confirmations})
				if minErr != nil {
					return minErr
				}
				if !minimized.Stable {
					return fmt.Errorf("minimization for %s did not reproduce %s stably", schedule.ID, target.Digest)
				}
				final, finalErr := runner.RunScheduleWithProvenance(cmd.Context(), spec, minimized.Minimized)
				if finalErr != nil {
					return fmt.Errorf("rerun minimized schedule: %w", finalErr)
				}
				if _, found := selectSignature(final.Signatures, target.Digest); !found {
					return fmt.Errorf("final minimized execution did not reproduce signature %s", target.Digest)
				}
				if _, buildErr := runner.BuildCapsule(spec, minimized.Minimized, final, minimized, destination); buildErr != nil {
					return fmt.Errorf("write minimized capsule: %w", buildErr)
				}
				out := minimizeOutput{Campaign: spec.Name, Schedule: minimized.Minimized, Signature: target, Minimization: minimized, Capsule: destination}
				if outputJSON {
					if err := json.NewEncoder(cmd.OutOrStdout()).Encode(out); err != nil {
						return err
					}
				} else {
					fmt.Fprintf(cmd.OutOrStdout(), "minimized %s\n  signature: %s\n  capsule: %s\n  stable: %t\n", schedule.ID, target.Digest, destination, minimized.Stable)
				}
				return ErrViolation
			}
			if unsignedViolation {
				return fmt.Errorf("campaign %q found a violation, but minimization is unavailable: no supported effect witness with a stable idempotency identity", spec.Name)
			}
			if requestedDigest != "" {
				return fmt.Errorf("no violation with signature %s was found", requestedDigest)
			}
			return fmt.Errorf("campaign %q produced no violation with a supported effect witness to minimize", spec.Name)
		},
	}
	command.Flags().StringVarP(&campaignPath, "campaign", "c", "", "path to campaign YAML")
	command.Flags().StringVarP(&outputDirectory, "output", "o", "", "new capsule directory, relative to the repository")
	command.Flags().StringVar(&requestedDigest, "signature", "", "minimize only this failure-signature digest")
	command.Flags().IntVar(&maxAttempts, "max-attempts", 100, "maximum candidate executions")
	command.Flags().IntVar(&confirmations, "confirmations", 3, "exact-signature confirmation runs")
	command.Flags().BoolVar(&outputJSON, "json", false, "emit machine-readable JSON")
	_ = command.MarkFlagRequired("campaign")
	_ = command.MarkFlagRequired("output")
	return command
}

func selectSignature(values []signature.Signature, requested string) (signature.Signature, bool) {
	for _, value := range values {
		if requested == "" || value.Digest == requested {
			return value, true
		}
	}
	return signature.Signature{}, false
}

func localNewDirectory(base, output string) (string, error) {
	if strings.TrimSpace(output) == "" {
		return "", fmt.Errorf("output directory is required")
	}
	if filepath.IsAbs(output) {
		return "", fmt.Errorf("output directory must be relative to the repository")
	}
	destination := filepath.Clean(filepath.Join(base, output))
	relative, err := filepath.Rel(base, destination)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("output directory must be a child of the repository")
	}
	if _, err := os.Lstat(destination); err == nil {
		return "", fmt.Errorf("output directory already exists: %s", output)
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("inspect output directory: %w", err)
	}
	return destination, nil
}
