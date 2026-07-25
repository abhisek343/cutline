package cli

import (
	"errors"
	"fmt"
	"io"

	"github.com/abhisek343/cutline/internal/buildinfo"
	"github.com/spf13/cobra"
)

var ErrNotImplemented = errors.New("command not implemented in this milestone")

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
		placeholderCommand("minimize", "minimize a failing schedule"),
		placeholderCommand("replay", "replay a failure capsule"),
		placeholderCommand("report", "render a run report"),
	)
	return root
}

func placeholderCommand(use, short string) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return fmt.Errorf("%s: %w", use, ErrNotImplemented)
		},
	}
}
