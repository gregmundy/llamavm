// Package version owns the on-disk layout under <home>/.llamavm/.
package version

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

var (
	// ErrNoActiveVersion means ~/.llamavm/current is missing or empty.
	ErrNoActiveVersion = errors.New("no active version")
	// ErrNotInstalled means the requested version directory does not exist.
	ErrNotInstalled = errors.New("version not installed")
)

// Store is rooted at <home>/.llamavm. <home> is normally os.UserHomeDir().
type Store struct {
	home string
}

// New returns a Store rooted under home (which should be the user's home directory).
func New(home string) *Store {
	return &Store{home: home}
}

func (s *Store) Root() string        { return filepath.Join(s.home, ".llamavm") }
func (s *Store) VersionsDir() string { return filepath.Join(s.Root(), "versions") }
func (s *Store) LogsDir() string     { return filepath.Join(s.Root(), "logs") }
func (s *Store) ShimsDir() string    { return filepath.Join(s.Root(), "shims") }
func (s *Store) CurrentFile() string { return filepath.Join(s.Root(), "current") }

// VersionDir is the final install directory for a tag.
func (s *Store) VersionDir(tag string) string {
	return filepath.Join(s.VersionsDir(), tag)
}

// StagingDir is the work-in-progress directory for an install. It is renamed
// onto VersionDir on success and removed on failure. The leading dot keeps it
// out of List() so a partial install never appears.
func (s *Store) StagingDir(tag string) string {
	return filepath.Join(s.VersionsDir(), ".staging-"+tag)
}

// IsInstalled reports whether the final version directory exists.
func (s *Store) IsInstalled(tag string) bool {
	info, err := os.Stat(s.VersionDir(tag))
	return err == nil && info.IsDir()
}

// List returns installed tags sorted ascending. Hidden entries (leading '.')
// are skipped so staging directories never surface.
func (s *Store) List() ([]string, error) {
	entries, err := os.ReadDir(s.VersionsDir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read versions dir: %w", err)
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() || strings.HasPrefix(name, ".") {
			continue
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out, nil
}

// Active returns the tag in ~/.llamavm/current. Surrounding whitespace is trimmed.
// Returns ErrNoActiveVersion when the file is missing or empty.
func (s *Store) Active() (string, error) {
	b, err := os.ReadFile(s.CurrentFile())
	if err != nil {
		if os.IsNotExist(err) {
			return "", ErrNoActiveVersion
		}
		return "", fmt.Errorf("read current: %w", err)
	}
	tag := strings.TrimSpace(string(b))
	if tag == "" {
		return "", ErrNoActiveVersion
	}
	return tag, nil
}

// SetActive writes tag to ~/.llamavm/current atomically (temp + rename).
func (s *Store) SetActive(tag string) error {
	if err := os.MkdirAll(s.Root(), 0o755); err != nil {
		return fmt.Errorf("ensure root: %w", err)
	}
	tmp, err := os.CreateTemp(s.Root(), ".current-*")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.WriteString(tag + "\n"); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("write temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("close temp: %w", err)
	}
	if err := os.Rename(tmpName, s.CurrentFile()); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("rename current: %w", err)
	}
	return nil
}

// ClearActive removes ~/.llamavm/current. Missing file is not an error.
func (s *Store) ClearActive() error {
	err := os.Remove(s.CurrentFile())
	if err == nil || os.IsNotExist(err) {
		return nil
	}
	return fmt.Errorf("remove current: %w", err)
}

// Remove deletes the final version directory. Returns ErrNotInstalled when absent.
func (s *Store) Remove(tag string) error {
	if !s.IsInstalled(tag) {
		return ErrNotInstalled
	}
	if err := os.RemoveAll(s.VersionDir(tag)); err != nil {
		return fmt.Errorf("remove version dir: %w", err)
	}
	return nil
}

// PromoteStaging renames the staging directory onto the final version directory.
func (s *Store) PromoteStaging(tag string) error {
	if err := os.MkdirAll(s.VersionsDir(), 0o755); err != nil {
		return fmt.Errorf("ensure versions dir: %w", err)
	}
	if err := os.Rename(s.StagingDir(tag), s.VersionDir(tag)); err != nil {
		return fmt.Errorf("promote staging: %w", err)
	}
	return nil
}

// RemoveStaging deletes the staging directory. Missing dir is not an error.
func (s *Store) RemoveStaging(tag string) error {
	if err := os.RemoveAll(s.StagingDir(tag)); err != nil {
		return fmt.Errorf("remove staging: %w", err)
	}
	return nil
}
