package cli

import (
	"encoding/json"
	"fmt"

	temporaladapter "github.com/abhisek343/cutline/internal/adapters/temporal"
	"github.com/spf13/cobra"
)

func newTemporalCommand() *cobra.Command {
	command := &cobra.Command{Use: "temporal", Short: "read evidence from a local Temporal workflow"}
	command.AddCommand(newTemporalInspectCommand())
	return command
}

func newTemporalInspectCommand() *cobra.Command {
	var address, namespace, workflowID, runID string
	var outputJSON bool
	command := &cobra.Command{
		Use:   "inspect",
		Short: "fetch and translate one local Temporal workflow history",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			snapshot, err := temporaladapter.Fetch(cmd.Context(), temporaladapter.LiveOptions{
				Address: address, Namespace: namespace, WorkflowID: workflowID, RunID: runID,
			})
			if err != nil {
				return err
			}
			if outputJSON {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(snapshot)
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Temporal workflow %q: events=%d complete=%t\n", workflowID, len(snapshot.Events), snapshot.Complete())
			return err
		},
	}
	command.Flags().StringVar(&address, "address", "127.0.0.1:7233", "local Temporal host:port")
	command.Flags().StringVar(&namespace, "namespace", "default", "Temporal namespace")
	command.Flags().StringVar(&workflowID, "workflow-id", "", "Temporal workflow ID")
	command.Flags().StringVar(&runID, "run-id", "", "optional Temporal execution run ID")
	command.Flags().BoolVar(&outputJSON, "json", false, "emit canonical evidence JSON")
	_ = command.MarkFlagRequired("workflow-id")
	return command
}
