package cli

import "github.com/spf13/cobra"

func newListCmd(_ *Deps) *cobra.Command {
	return &cobra.Command{Use: "list", Hidden: true, RunE: func(*cobra.Command, []string) error { return nil }}
}
