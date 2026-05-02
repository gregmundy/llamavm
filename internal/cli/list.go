package cli

import (
	"errors"
	"fmt"
	"sort"

	"github.com/spf13/cobra"

	"github.com/gregmundy/llamavm/internal/version"
)

func newListCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "Show installed versions",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runList(deps)
		},
	}
}

func runList(deps *Deps) error {
	tags, err := deps.Store.List()
	if err != nil {
		return fmt.Errorf("list versions: %w", err)
	}
	if len(tags) == 0 {
		fmt.Fprintln(deps.Stdout, "No versions installed. Run 'llamavm install latest' to install one.")
		return nil
	}
	sort.Strings(tags)

	active, err := deps.Store.Active()
	if err != nil && !errors.Is(err, version.ErrNoActiveVersion) {
		return fmt.Errorf("read active: %w", err)
	}

	for _, t := range tags {
		marker := "  "
		if t == active {
			marker = "* "
		}
		fmt.Fprintf(deps.Stdout, "%s%s\n", marker, t)
	}
	return nil
}
