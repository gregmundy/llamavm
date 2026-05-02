package cli

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/gregmundy/llamavm/internal/version"
)

func newUninstallCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall <version>",
		Short: "Remove an installed version",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUninstall(deps, args[0])
		},
	}
}

func runUninstall(deps *Deps, tag string) error {
	if !deps.Store.IsInstalled(tag) {
		return fmt.Errorf("%s is not installed", tag)
	}
	if err := deps.Store.Remove(tag); err != nil {
		if errors.Is(err, version.ErrNotInstalled) {
			return fmt.Errorf("%s is not installed", tag)
		}
		return fmt.Errorf("remove %s: %w", tag, err)
	}

	active, err := deps.Store.Active()
	if err != nil && !errors.Is(err, version.ErrNoActiveVersion) {
		return fmt.Errorf("read active: %w", err)
	}
	if active == tag {
		if err := deps.Store.ClearActive(); err != nil {
			return fmt.Errorf("clear active: %w", err)
		}
	}

	fmt.Fprintf(deps.Stdout, "Uninstalled %s\n", tag)
	return nil
}
