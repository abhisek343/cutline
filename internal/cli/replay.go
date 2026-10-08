package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/abhisek343/cutline/internal/adapters/native"
	"github.com/abhisek343/cutline/internal/adapters/temporal"
	"github.com/abhisek343/cutline/internal/campaign"
	"github.com/abhisek343/cutline/internal/capsule"
	"github.com/abhisek343/cutline/internal/replay"
	"github.com/spf13/cobra"
)

func newReplayCommand() *cobra.Command {
	var outputJSON, comparative bool
	var campaignPath string
	command := &cobra.Command{
		Use:   "replay <capsule-directory>",
		Short: "replay a failure capsule",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			base, err := os.Getwd()
			if err != nil {
				return err
			}
			manifest, err := capsule.ValidateDirectory(args[0])
			if err != nil {
				return err
			}
			config := replay.Config{BaseDirectory: base, AllowTargetMismatch: comparative}
			if campaignPath != "" {
				spec, loadErr := campaign.LoadFile(campaignPath)
				if loadErr != nil {
					return loadErr
				}
				config.Campaign = &spec
			}
			var result replay.Result
			if manifest.Adapter == "temporal" {
				result, err = (temporal.Runner{BaseDirectory: base}).ReplayCapsule(cmd.Context(), args[0], config)
			} else {
				result, err = (native.Runner{BaseDirectory: base}).ReplayCapsule(cmd.Context(), args[0], config)
			}
			if err != nil {
				return err
			}
			if outputJSON {
				if err := json.NewEncoder(cmd.OutOrStdout()).Encode(result); err != nil {
					return err
				}
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "replay: %s\n  expected: %s\n", result.Status, result.Expected.Digest)
				for _, value := range result.Observed {
					fmt.Fprintf(cmd.OutOrStdout(), "  observed: %s\n", value.Digest)
				}
			}
			if result.Exact || result.Status == replay.StatusComparative {
				return ErrViolation
			}
			return ErrInconclusive
		},
	}
	command.Flags().BoolVar(&comparative, "comparative", false, "explicitly compare against an intentionally changed target, campaign, or adapter")
	command.Flags().StringVarP(&campaignPath, "campaign", "c", "", "replacement campaign for comparative replay")
	command.Flags().BoolVar(&outputJSON, "json", false, "emit machine-readable JSON")
	return command
}
