package version

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// PinFileName is the per-directory pin file (PRD §3.13).
const PinFileName = ".llama-version"

// Resolver answers "which tag should a shim invocation use?".
// PRD §3.8 / §3.12.2 resolution order:
//  1. .llama-version in cwd or any ancestor up to (and including) home.
//  2. ~/.llamavm/current.
//  3. ErrNoActiveVersion.
type Resolver struct {
	store *Store
	// home bounds the cwd-walk: once Resolve has examined this directory,
	// it stops walking. Empty home disables the boundary (walk stops at
	// the filesystem root). Pass os.UserHomeDir() in production.
	home string
}

// NewResolver wraps a Store with a walk boundary. The store must be non-nil.
// home is the directory where the cwd-walk stops; pass os.UserHomeDir() in
// production.
func NewResolver(s *Store, home string) *Resolver {
	return &Resolver{store: s, home: home}
}

// Resolve returns the active tag for shim invocations rooted at cwd.
// If cwd is empty, the walk is skipped and only ~/.llamavm/current is consulted
// — useful for tests and for callers that have no meaningful cwd.
func (r *Resolver) Resolve(cwd string) (string, error) {
	if cwd != "" {
		tag, found, err := r.findInAncestors(cwd)
		if err != nil {
			return "", err
		}
		if found {
			return tag, nil
		}
	}
	return r.store.Active()
}

// findInAncestors walks dir → parent → ... checking for PinFileName at each
// level. Stops after checking r.home (or after the filesystem root if home is
// empty or cwd is outside the home tree). Returns (tag, true, nil) on a match,
// ("", false, nil) on no match, or an error on read failures other than
// fs.ErrNotExist.
func (r *Resolver) findInAncestors(start string) (string, bool, error) {
	dir := start
	for {
		candidate := filepath.Join(dir, PinFileName)
		b, err := os.ReadFile(candidate)
		switch {
		case err == nil:
			tag := strings.TrimSpace(string(b))
			if tag != "" {
				return tag, true, nil
			}
			// Empty/whitespace pin file: treat as no pin and keep walking.
		case errors.Is(err, fs.ErrNotExist):
			// Not pinned at this level; keep walking.
		default:
			return "", false, fmt.Errorf("read %s: %w", candidate, err)
		}
		if r.home != "" && dir == r.home {
			return "", false, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false, nil
		}
		dir = parent
	}
}
