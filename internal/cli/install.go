package cli

import "github.com/spf13/cobra"

func newInstallCmd(_ *Deps) *cobra.Command {
	return &cobra.Command{Use: "install", Hidden: true, RunE: func(*cobra.Command, []string) error { return nil }}
}
