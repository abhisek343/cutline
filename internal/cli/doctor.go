package cli

import (
	"encoding/json"
	"fmt"

	"github.com/abhisek343/cutline/internal/buildinfo"
	"github.com/spf13/cobra"
)

func newDoctorCommand(info buildinfo.Info) *cobra.Command {
	var asJSON bool
	command := &cobra.Command{
		Use:   "doctor",
		Short: "inspect the local Cutline runtime",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			result := struct {
				Build       buildinfo.Info `json:"build"`
				NativeReady bool           `json:"nativeReady"`
				UnixSocket  bool           `json:"unixSocket"`
			}{
				Build:       info,
				NativeReady: info.OS == "linux",
				UnixSocket:  info.OS != "windows",
			}
			if asJSON {
				encoder := json.NewEncoder(cmd.OutOrStdout())
				encoder.SetEscapeHTML(true)
				return encoder.Encode(result)
			}
			_, err := fmt.Fprintf(
				cmd.OutOrStdout(),
				"Cutline %s (%s/%s, %s)\nnative adapter: %t\nunix control socket: %t\n",
				info.Version,
				info.OS,
				info.Arch,
				info.GoVersion,
				result.NativeReady,
				result.UnixSocket,
			)
			return err
		},
	}
	command.Flags().BoolVar(&asJSON, "json", false, "emit machine-readable JSON")
	return command
}
