package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/gregmundy/llamavm/internal/version"
)

func newPinCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "pin <version>",
		Short: "Write .llama-version in the current directory",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPin(deps, args[0])
		},
	}
}

func runPin(deps *Deps, tag string) error {
	// Getwd failure is an environment fault (deleted cwd, perms) — not user
	// input, so it is not wrapped with ErrUserError.
	cwd, err := deps.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}
	// PRD §3.9: warn but still write when the tag is not installed; users may
	// pin a tag they intend to install later.
	if !deps.Store.IsInstalled(tag) {
		fmt.Fprintf(deps.Stderr,
			"warning: %s is not currently installed; run 'llamavm install %s' to install it\n",
			tag, tag)
	}
	target := filepath.Join(cwd, version.PinFileName)
	if err := writePinFile(target, tag); err != nil {
		return fmt.Errorf("write %s: %w", target, err)
	}
	fmt.Fprintf(deps.Stdout, "Pinned %s in %s\n", tag, target)
	return nil
}

// writePinFile writes "<tag>\n" to dst atomically (temp + rename).
func writePinFile(dst, tag string) error {
	dir := filepath.Dir(dst)
	tmp, err := os.CreateTemp(dir, ".llama-version-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.WriteString(tag + "\n"); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, dst); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}
