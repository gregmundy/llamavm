package cli

import (
	"context"
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	gh "github.com/gregmundy/llamavm/internal/github"
)

func newListRemoteCmd(deps *Deps) *cobra.Command {
	var limit int
	var all bool
	cmd := &cobra.Command{
		Use:   "list-remote",
		Short: "Show available llama.cpp release tags from GitHub (most recent first)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runListRemote(cmd.Context(), deps, limit, all)
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 30, "number of tags to show (ignored with --all)")
	cmd.Flags().BoolVar(&all, "all", false, "list every release tag (potentially hundreds)")
	return cmd
}

func runListRemote(ctx context.Context, deps *Deps, limit int, all bool) error {
	if limit < 1 && !all {
		return fmt.Errorf("--limit must be >= 1: %w", ErrUserError)
	}
	tags, err := deps.GitHub.ListReleases(ctx, limit, all)
	if err != nil {
		if errors.Is(err, gh.ErrRateLimited) {
			return fmt.Errorf("github rate limited; set GITHUB_TOKEN to raise the limit: %w", ErrUserError)
		}
		return fmt.Errorf("list releases: %w", err)
	}
	for i, tag := range tags {
		if i == 0 {
			fmt.Fprintf(deps.Stdout, "%s (latest)\n", tag)
		} else {
			fmt.Fprintln(deps.Stdout, tag)
		}
	}
	return nil
}
