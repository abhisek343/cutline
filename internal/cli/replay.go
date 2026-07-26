package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/abhisek343/cutline/internal/adapters/native"
	"github.com/abhisek343/cutline/internal/replay"
	"github.com/spf13/cobra"
)

func newReplayCommand() *cobra.Command {
	var outputJSON bool
	command := &cobra.Command{
		Use:   "replay <capsule-directory>",
		Short: "replay a failure capsule",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			base, err := os.Getwd()
			if err != nil {
				return err
			}
			result, err := (native.Runner{BaseDirectory: base}).ReplayCapsule(cmd.Context(), args[0], replay.Config{})
			if err != nil {
				return err
			}
			if outputJSON {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(result)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "replay: %s\n  expected: %s\n", result.Status, result.Expected.Digest)
			for _, value := range result.Observed {
				fmt.Fprintf(cmd.OutOrStdout(), "  observed: %s\n", value.Digest)
			}
			if result.Exact {
				return ErrViolation
			}
			return ErrInconclusive
		},
	}
	command.Flags().BoolVar(&outputJSON, "json", false, "emit machine-readable JSON")
	return command
}
