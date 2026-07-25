package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/abhisek343/cutline/internal/campaign"
	"github.com/spf13/cobra"
)

func newRunCommand() *cobra.Command {
	var campaignPath string
	var planJSON bool
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
			if planJSON {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(plan)
			}
			_, err = fmt.Fprintf(
				cmd.OutOrStdout(),
				"campaign %q validated\n  digest: %s\n  adapter: %s\n  command: %s\n  max schedules: %d\n",
				plan.Name,
				plan.Digest,
				plan.Adapter,
				strings.Join(plan.Command, " "),
				plan.MaxSchedules,
			)
			return err
		},
	}
	command.Flags().StringVarP(&campaignPath, "campaign", "c", "", "path to campaign YAML")
	command.Flags().BoolVar(&planJSON, "json", false, "emit the validated execution plan as JSON")
	_ = command.MarkFlagRequired("campaign")
	return command
}
