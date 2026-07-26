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
			result, err := runner.Run(cmd.Context(), spec)
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
					"run %s: %s\n  attempt: %s\n  events: %d\n  effects: %d\n  duration: %s\n",
					result.RunID,
					result.Status,
					result.AttemptID,
					len(result.Evidence.Events),
					len(result.Effects),
					result.Duration,
				)
				for _, evaluation := range result.Evaluations {
					fmt.Fprintf(
						cmd.OutOrStdout(),
						"  contract %s: %s — %s\n",
						evaluation.Contract,
						evaluation.Status,
						evaluation.Message,
					)
				}
				if result.Stdout != "" {
					fmt.Fprintf(cmd.OutOrStdout(), "  target stdout:\n%s", indent(result.Stdout))
				}
				if result.Stderr != "" {
					fmt.Fprintf(cmd.OutOrStdout(), "  target stderr:\n%s", indent(result.Stderr))
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
