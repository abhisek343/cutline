package cli

import (
	"errors"
	"io"

	"github.com/abhisek343/cutline/internal/buildinfo"
	"github.com/spf13/cobra"
)

var (
	ErrViolation    = errors.New("cutline found a contract violation")
	ErrInconclusive = errors.New("cutline run is inconclusive")
	ErrInvalidRun   = errors.New("cutline run is invalid")
)

// New constructs an isolated command tree suitable for tests and embedding.
func New(stdout, stderr io.Writer) *cobra.Command {
	info := buildinfo.Current()
	root := &cobra.Command{
		Use:           "cutline",
		Short:         "Deterministic cancellation-correctness testing",
		Version:       info.Version,
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.AddCommand(
		newDoctorCommand(info),
		newRunCommand(),
		newMinimizeCommand(),
		newReplayCommand(),
		newReportCommand(),
	)
	return root
}

// ExitCode maps a semantic CLI result to a stable process status.
func ExitCode(err error) int {
	switch {
	case err == nil:
		return 0
	case errors.Is(err, ErrViolation):
		return 2
	case errors.Is(err, ErrInconclusive):
		return 3
	case errors.Is(err, ErrInvalidRun):
		return 4
	default:
		return 1
	}
}
