package cli

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/gregmundy/llamavm/internal/version"
)

func newCurrentCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "current",
		Short: "Print the active llama.cpp version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runCurrent(deps)
		},
	}
}

func runCurrent(deps *Deps) error {
	tag, err := deps.Resolver.Resolve()
	if err != nil {
		if errors.Is(err, version.ErrNoActiveVersion) {
			return fmt.Errorf("No active version: %w", ErrUserError)
		}
		return fmt.Errorf("resolve active version: %w", err)
	}
	fmt.Fprintln(deps.Stdout, tag)
	return nil
}
