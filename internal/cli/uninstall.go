package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
		return fmt.Errorf("%s is not installed: %w", tag, ErrUserError)
	}
	if err := deps.Store.Remove(tag); err != nil {
		if errors.Is(err, version.ErrNotInstalled) {
			return fmt.Errorf("%s is not installed: %w", tag, ErrUserError)
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

	// Garbage-collect llama-* shims that no remaining installed version
	// provides. With dynamic shim discovery, an old version may have
	// installed shims for a tool no remaining version ships (e.g. a
	// renamed binary upstream). Leave shims that are still backed by at
	// least one install — they'll resolve to whichever version is active.
	if err := cleanupOrphanedShims(deps); err != nil {
		// Non-fatal: the uninstall itself succeeded; surface the warning
		// but don't fail the command.
		fmt.Fprintf(deps.Stderr, "llamavm: warning: cleaning up shims: %v\n", err)
	}

	fmt.Fprintf(deps.Stdout, "Uninstalled %s\n", tag)
	return nil
}

// cleanupOrphanedShims removes any llama-* shim in the shims dir that is
// not provided by at least one currently-installed version's bin/. Skips
// non-llama-* entries (defensive — we never created them) and tolerates
// individual remove errors with a single warning line per orphan.
func cleanupOrphanedShims(deps *Deps) error {
	shimsDir := deps.Store.ShimsDir()
	entries, err := os.ReadDir(shimsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read shims dir: %w", err)
	}
	tags, err := deps.Store.List()
	if err != nil {
		return fmt.Errorf("list installed: %w", err)
	}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, llamaBinaryPrefix) {
			continue
		}
		provided := false
		for _, t := range tags {
			if _, err := os.Stat(filepath.Join(deps.Store.VersionDir(t), "bin", name)); err == nil {
				provided = true
				break
			}
		}
		if !provided {
			if err := os.Remove(filepath.Join(shimsDir, name)); err != nil && !os.IsNotExist(err) {
				fmt.Fprintf(deps.Stderr, "llamavm: warning: remove orphan shim %s: %v\n", name, err)
			}
		}
	}
	return nil
}
