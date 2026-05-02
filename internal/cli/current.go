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
	// Getwd failure is an environment fault (deleted cwd, perms) — not user
	// input, so it is not wrapped with ErrUserError.
	cwd, err := deps.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}
	tag, err := deps.Resolver.Resolve(cwd)
	if err != nil {
		if errors.Is(err, version.ErrNoActiveVersion) {
			return fmt.Errorf("No active version: %w", ErrUserError)
		}
		return fmt.Errorf("resolve active version: %w", err)
	}
	fmt.Fprintln(deps.Stdout, tag)
	return nil
}
