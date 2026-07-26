package cli

import (
	"fmt"

	"github.com/abhisek343/cutline/internal/report"
	"github.com/spf13/cobra"
)

func newReportCommand() *cobra.Command {
	var format, output string
	command := &cobra.Command{
		Use:   "report <capsule-directory>",
		Short: "render a static capsule report",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if output == "" {
				data, err := report.Render(args[0], format)
				if err != nil {
					return err
				}
				_, err = cmd.OutOrStdout().Write(data)
				return err
			}
			if err := report.Write(args[0], output, format); err != nil {
				return err
			}
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "report written to %s\n", output)
			return err
		},
	}
	command.Flags().StringVar(&format, "format", "html", "report format: html or json")
	command.Flags().StringVarP(&output, "output", "o", "", "write to a new file instead of stdout")
	return command
}
