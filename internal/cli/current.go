package cli

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/gregmundy/llamavm/internal/version"
)

func newCurrentCmd(deps *Deps) *cobra.Command {
	var verbose bool
	cmd := &cobra.Command{
		Use:   "current",
		Short: "Print the active llama.cpp version",
		Long: "Print the active llama.cpp version. With --verbose, also show " +
			"where the version was resolved from (a .llama-version pin in the " +
			"current directory's ancestry, or the global ~/.llamavm/current file).",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runCurrent(deps, verbose)
		},
	}
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "show where the version came from")
	return cmd
}

func runCurrent(deps *Deps, verbose bool) error {
	// Getwd failure is an environment fault (deleted cwd, perms) — not user
	// input, so it is not wrapped with ErrUserError.
	cwd, err := deps.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}
	res, err := deps.Resolver.ResolveDetailed(cwd)
	if err != nil {
		if errors.Is(err, version.ErrNoActiveVersion) {
			return fmt.Errorf("no active version: %w", ErrUserError)
		}
		return fmt.Errorf("resolve active version: %w", err)
	}
	if !verbose {
		fmt.Fprintln(deps.Stdout, res.Tag)
		return nil
	}
	switch res.Source {
	case version.SourcePin:
		fmt.Fprintf(deps.Stdout, "%s (pinned at %s)\n", res.Tag, res.Path)
	case version.SourceCurrent:
		fmt.Fprintf(deps.Stdout, "%s (from %s)\n", res.Tag, res.Path)
	default:
		// Shouldn't happen given the enum, but be defensive.
		fmt.Fprintln(deps.Stdout, res.Tag)
	}
	return nil
}
