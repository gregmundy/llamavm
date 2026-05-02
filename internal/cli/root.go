package cli

import (
	"github.com/spf13/cobra"
)

// NewRoot returns the root cobra command with all M1 subcommands wired up.
// Pass the llamavm version string (e.g. "v1.0.0") for `--version`.
func NewRoot(deps *Deps, llamavmVersion string) *cobra.Command {
	root := &cobra.Command{
		Use:           "llamavm",
		Short:         "A version manager for llama.cpp on Apple Silicon",
		Version:       llamavmVersion,
		SilenceUsage:  true,
		SilenceErrors: false,
	}
	root.SetOut(deps.Stdout)
	root.SetErr(deps.Stderr)

	root.AddCommand(newInstallCmd(deps))
	root.AddCommand(newUninstallCmd(deps))
	root.AddCommand(newListCmd(deps))
	root.AddCommand(newCurrentCmd(deps))
	root.AddCommand(newUseCmd(deps))
	root.AddCommand(newPinCmd(deps))
	return root
}
