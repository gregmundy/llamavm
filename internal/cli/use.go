package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newUseCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "use <version>",
		Short: "Set the global active llama.cpp version",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUse(deps, args[0])
		},
	}
}

func runUse(deps *Deps, tag string) error {
	if !deps.Store.IsInstalled(tag) {
		return fmt.Errorf("%s is not installed; run 'llamavm install %s' first: %w", tag, tag, ErrUserError)
	}
	if err := deps.Store.SetActive(tag); err != nil {
		return fmt.Errorf("set active: %w", err)
	}
	fmt.Fprintf(deps.Stdout, "Active version: %s\n", tag)
	return nil
}
