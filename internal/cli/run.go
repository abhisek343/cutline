package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/abhisek343/cutline/internal/adapters/native"
	"github.com/abhisek343/cutline/internal/campaign"
	"github.com/spf13/cobra"
)

func newRunCommand() *cobra.Command {
	var campaignPath string
	var outputJSON bool
	var validateOnly bool
	command := &cobra.Command{
		Use:   "run",
		Short: "validate a campaign and run its schedules",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			spec, err := campaign.LoadFile(campaignPath)
			if err != nil {
				return err
			}
			digest, err := spec.Digest()
			if err != nil {
				return err
			}
			if validateOnly {
				return printPlan(cmd, spec, digest, outputJSON)
			}
			baseDirectory, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("resolve current working directory: %w", err)
			}
			runner, err := runnerForCampaign(spec, baseDirectory)
			if err != nil {
				return err
			}
			result, err := runner.RunCampaign(cmd.Context(), spec)
			if err != nil {
				return err
			}
			if outputJSON {
				if err := json.NewEncoder(cmd.OutOrStdout()).Encode(result); err != nil {
					return err
				}
			} else {
				fmt.Fprintf(
					cmd.OutOrStdout(),
					"campaign %q: %s\n  discovery run: %s\n  discovered checkpoints: %d\n  schedules: %d\n",
					spec.Name,
					result.Status,
					result.Discovery.Status,
					len(result.Plan.Checkpoints),
					len(result.Schedules),
				)
				for index, schedule := range result.Schedules {
					planned := result.Plan.Schedules[index]
					description := planned.CancelAt
					if planned.PairSide != "" {
						description += " " + planned.PairSide + " " + planned.PairPoint
					}
					if len(planned.ReleasePrefix) > 0 {
						description += " prefix=" + strings.Join(planned.ReleasePrefix, ",")
					}
					fmt.Fprintf(cmd.OutOrStdout(), "  schedule %d (%s): %s, events=%d, effects=%d\n", index+1, description, schedule.Status, len(schedule.Evidence.Events), len(schedule.Effects))
					for _, evaluation := range schedule.Evaluations {
						fmt.Fprintf(cmd.OutOrStdout(), "    contract %s: %s — %s\n", evaluation.Contract, evaluation.Status, evaluation.Message)
					}
				}
				if len(result.Plan.Unreachable) > 0 {
					fmt.Fprintf(cmd.OutOrStdout(), "  unreachable checkpoints: %s\n", strings.Join(result.Plan.Unreachable, ", "))
				}
			}
			switch result.Status {
			case native.OverallPass:
				return nil
			case native.OverallViolation:
				return ErrViolation
			case native.OverallInconclusive:
				return ErrInconclusive
			case native.OverallInvalid:
				return ErrInvalidRun
			default:
				return errors.New("unknown native run status")
			}
		},
	}
	command.Flags().StringVarP(&campaignPath, "campaign", "c", "", "path to campaign YAML")
	command.Flags().BoolVar(&outputJSON, "json", false, "emit machine-readable JSON")
	command.Flags().BoolVar(&validateOnly, "validate-only", false, "validate and print the execution plan without running")
	_ = command.MarkFlagRequired("campaign")
	return command
}

func printPlan(cmd *cobra.Command, spec campaign.Campaign, digest string, asJSON bool) error {
	plan := struct {
		Name         string   `json:"name"`
		Digest       string   `json:"digest"`
		Adapter      string   `json:"adapter"`
		Command      []string `json:"command"`
		MaxSchedules int      `json:"maxSchedules"`
		Status       string   `json:"status"`
	}{
		Name:         spec.Name,
		Digest:       digest,
		Adapter:      spec.Target.Adapter,
		Command:      spec.Target.Command,
		MaxSchedules: spec.Exploration.MaxSchedules,
		Status:       "validated",
	}
	if asJSON {
		return json.NewEncoder(cmd.OutOrStdout()).Encode(plan)
	}
	_, err := fmt.Fprintf(
		cmd.OutOrStdout(),
		"campaign %q validated\n  digest: %s\n  adapter: %s\n  command: %s\n  max schedules: %d\n",
		plan.Name,
		plan.Digest,
		plan.Adapter,
		strings.Join(plan.Command, " "),
		plan.MaxSchedules,
	)
	return err
}

func indent(value string) string {
	value = strings.TrimSuffix(value, "\n")
	if value == "" {
		return ""
	}
	return "    " + strings.ReplaceAll(value, "\n", "\n    ") + "\n"
}
