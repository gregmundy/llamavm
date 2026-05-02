package cli

import "github.com/spf13/cobra"

func newUninstallCmd(_ *Deps) *cobra.Command {
	return &cobra.Command{Use: "uninstall", Hidden: true, RunE: func(*cobra.Command, []string) error { return nil }}
}
