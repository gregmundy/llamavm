package shim

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Installer materializes shim binaries by copying a single source binary
// to ~/.llamavm/shims/<name> for each name in Names.
type Installer struct {
	// Source returns the on-disk path of the llamavm-shim binary. Lazy
	// because the path is only needed when at least one shim is missing.
	Source func() (string, error)
}

// EnsureInstalled is idempotent: it creates shimsDir if needed and
// copies the source binary for any name in Names that is not already
// present. Pre-existing shims are left untouched (lets users keep an
// older shim binary working after a llamavm upgrade if they pin it).
func (i *Installer) EnsureInstalled(shimsDir string) error {
	if err := os.MkdirAll(shimsDir, 0o755); err != nil {
		return fmt.Errorf("create shims dir: %w", err)
	}
	missing := make([]string, 0, len(Names))
	for _, name := range Names {
		if _, err := os.Stat(filepath.Join(shimsDir, name)); err == nil {
			continue
		}
		missing = append(missing, name)
	}
	if len(missing) == 0 {
		return nil
	}
	src, err := i.Source()
	if err != nil {
		return fmt.Errorf("locate shim binary: %w", err)
	}
	for _, name := range missing {
		if err := installOne(src, filepath.Join(shimsDir, name)); err != nil {
			return fmt.Errorf("install shim %s: %w", name, err)
		}
	}
	return nil
}

// installOne copies src to dst atomically: write to a sibling temp file,
// chmod 0755, rename. Rename is atomic on the same filesystem.
func installOne(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	tmp, err := os.CreateTemp(filepath.Dir(dst), ".shim-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := io.Copy(tmp, in); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Chmod(tmpName, 0o755); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, dst); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}
