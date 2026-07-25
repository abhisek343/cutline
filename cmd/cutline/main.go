package main

import (
	"fmt"
	"os"

	"github.com/abhisek343/cutline/internal/cli"
)

func main() {
	command := cli.New(os.Stdout, os.Stderr)
	if err := command.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
