# llamavm M3 — `.llama-version` resolution + `pin` Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement per-project version pinning. After M3: `Resolver.Resolve(cwd)` walks from cwd through ancestors looking for `.llama-version`, stopping after checking the user's home directory, before falling back to `~/.llamavm/current`. The `current` subcommand and the shim binary both pass cwd in. The new `pin` subcommand writes `.llama-version` in cwd (warning, not erroring, when the tag isn't installed yet).

**Architecture:** Three small layers extend M2 without restructuring it.
1. `version.Resolver` gains a `home` field (walk boundary) and a `Resolve(cwd string)` method. Empty cwd skips the walk and behaves like M2.
2. `cli.Resolver` interface signature changes to `Resolve(cwd string)`; `cli.Deps` gains `Getwd func() (string, error)` so subcommands can inject cwd in tests. `current` and the new `pin` use it.
3. The shim runner's `Options.Resolver` stays `func() (string, error)` — `cmd/llamavm-shim/main.go` adapts via a closure that calls `os.Getwd()` then `resolver.Resolve(cwd)`. This keeps the runner package indifferent to cwd plumbing.

**Tech Stack:** Go 1.22+, `github.com/spf13/cobra` v1.10.2, stdlib only (`os`, `io/fs`, `errors`, `path/filepath`, `strings`). No new deps.

**Out of scope (deferred):**
- `bench` (single + comparison) → M4
- `doctor`, Homebrew tap, README, release → M5
- Any `.llama-version` validation beyond "non-empty after trim" — pinned tags need not be installed (PRD §3.9). The shim's existing "binary not found" error covers misconfiguration at use time.

**PRD anchors:**
- §3.8 `current` resolution order — `.llama-version` first, then `~/.llamavm/current`, else exit non-zero.
- §3.9 `pin` — overwrites; warns (does not error) when the tag isn't installed.
- §3.12.2 Shim resolution — same walk; stop at user's home directory; `os.Getwd()` is the starting point.
- §3.13 `.llama-version` format — single-line ASCII tag; trailing whitespace/newlines tolerated when reading.

---

## File Structure

**Created:**
- `internal/cli/pin.go` — `pin <version>` subcommand.
- `internal/cli/pin_test.go`

**Modified:**
- `internal/version/resolver.go` — `Resolver` gains `home` field; `Resolve(cwd string) (string, error)` walks ancestors before falling back to `Store.Active()`.
- `internal/version/resolver_test.go` — existing tests pass `""` for cwd; new tests cover the walk, the home boundary, trimming, empty file, and missing-file fallback.
- `internal/cli/deps.go` — `Resolver` interface signature becomes `Resolve(cwd string) (string, error)`; `Deps` gains `Getwd func() (string, error)`.
- `internal/cli/list_test.go` — `fakeResolver.Resolve` signature update; resolver records last cwd it saw for assertions.
- `internal/cli/current.go` — calls `deps.Getwd()`, passes cwd to `Resolver.Resolve`. Surfaces Getwd errors as wrapped errors (not user errors).
- `internal/cli/current_test.go` — every existing test now sets `Deps.Getwd`; new tests cover cwd-aware resolution and Getwd failure.
- `internal/cli/root.go` — register `newPinCmd`.
- `cmd/llamavm/main.go` — `version.NewResolver(store, home)`; `Deps.Getwd: os.Getwd`.
- `cmd/llamavm-shim/main.go` — adapt closure that calls `os.Getwd()` then `resolver.Resolve(cwd)`; pass `home` into `NewResolver`.

**Disk layout after `llamavm pin b5046` from `~/projects/foo`:**

```
~/projects/foo/.llama-version   # contents: "b5046\n"
```

No other on-disk shape changes from M2.

---

## Task 1: Resolver `.llama-version` walk

**Files:**
- Modify: `internal/version/resolver.go`
- Modify: `internal/version/resolver_test.go`

- [ ] **Step 1: Update existing tests for the new signature**

Replace `internal/version/resolver_test.go` with:

```go
package version

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// writeFile is a small helper: write body (with trailing newline if nl) at path.
func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestResolver_NoActiveVersion(t *testing.T) {
	home := t.TempDir()
	r := NewResolver(New(home), home)
	if _, err := r.Resolve(""); !errors.Is(err, ErrNoActiveVersion) {
		t.Fatalf("Resolve(\"\") on empty: got %v, want ErrNoActiveVersion", err)
	}
}

func TestResolver_ReadsCurrentFileWhenNoCwd(t *testing.T) {
	home := t.TempDir()
	s := New(home)
	if err := s.SetActive("b5046"); err != nil {
		t.Fatal(err)
	}
	r := NewResolver(s, home)
	got, err := r.Resolve("")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != "b5046" {
		t.Fatalf("Resolve = %q, want b5046", got)
	}
}

func TestResolver_FindsLlamaVersionInCwd(t *testing.T) {
	home := t.TempDir()
	cwd := filepath.Join(home, "project")
	writeFile(t, filepath.Join(cwd, ".llama-version"), "b5489\n")

	r := NewResolver(New(home), home)
	got, err := r.Resolve(cwd)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != "b5489" {
		t.Fatalf("Resolve = %q, want b5489", got)
	}
}

func TestResolver_FindsLlamaVersionInAncestor(t *testing.T) {
	home := t.TempDir()
	parent := filepath.Join(home, "workspace")
	cwd := filepath.Join(parent, "deep", "deeper")
	writeFile(t, filepath.Join(parent, ".llama-version"), "b5400\n")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}

	r := NewResolver(New(home), home)
	got, err := r.Resolve(cwd)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != "b5400" {
		t.Fatalf("Resolve = %q, want b5400", got)
	}
}

func TestResolver_PrefersCwdOverAncestor(t *testing.T) {
	home := t.TempDir()
	parent := filepath.Join(home, "workspace")
	cwd := filepath.Join(parent, "child")
	writeFile(t, filepath.Join(parent, ".llama-version"), "b5400\n")
	writeFile(t, filepath.Join(cwd, ".llama-version"), "b5489\n")

	r := NewResolver(New(home), home)
	got, err := r.Resolve(cwd)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != "b5489" {
		t.Fatalf("Resolve = %q, want b5489 (cwd wins over ancestor)", got)
	}
}

func TestResolver_PrefersLlamaVersionOverCurrentFile(t *testing.T) {
	home := t.TempDir()
	cwd := filepath.Join(home, "project")
	writeFile(t, filepath.Join(cwd, ".llama-version"), "b5489\n")
	s := New(home)
	if err := s.SetActive("b5046"); err != nil {
		t.Fatal(err)
	}

	r := NewResolver(s, home)
	got, err := r.Resolve(cwd)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != "b5489" {
		t.Fatalf("Resolve = %q, want b5489 (.llama-version wins over current)", got)
	}
}

func TestResolver_StopsAtHome(t *testing.T) {
	// .llama-version is one level above home — must NOT be picked up.
	above := t.TempDir()
	home := filepath.Join(above, "home")
	cwd := filepath.Join(home, "project")
	writeFile(t, filepath.Join(above, ".llama-version"), "b9999\n")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}

	s := New(home)
	if err := s.SetActive("b5046"); err != nil {
		t.Fatal(err)
	}
	r := NewResolver(s, home)
	got, err := r.Resolve(cwd)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != "b5046" {
		t.Fatalf("Resolve = %q, want b5046 (walk should not cross home boundary)", got)
	}
}

func TestResolver_ChecksHomeItself(t *testing.T) {
	// .llama-version directly in home is in scope — the walk includes home.
	home := t.TempDir()
	writeFile(t, filepath.Join(home, ".llama-version"), "b5489\n")

	r := NewResolver(New(home), home)
	got, err := r.Resolve(home)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != "b5489" {
		t.Fatalf("Resolve = %q, want b5489", got)
	}
}

func TestResolver_TrimsWhitespace(t *testing.T) {
	home := t.TempDir()
	cwd := filepath.Join(home, "project")
	writeFile(t, filepath.Join(cwd, ".llama-version"), "  b5489\n\n")

	r := NewResolver(New(home), home)
	got, err := r.Resolve(cwd)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != "b5489" {
		t.Fatalf("Resolve = %q, want b5489 (trimmed)", got)
	}
}

func TestResolver_EmptyFileFallsThrough(t *testing.T) {
	// An empty .llama-version is treated as no pin: keep walking, then fall
	// back to ~/.llamavm/current.
	home := t.TempDir()
	cwd := filepath.Join(home, "project")
	writeFile(t, filepath.Join(cwd, ".llama-version"), "   \n")
	s := New(home)
	if err := s.SetActive("b5046"); err != nil {
		t.Fatal(err)
	}

	r := NewResolver(s, home)
	got, err := r.Resolve(cwd)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != "b5046" {
		t.Fatalf("Resolve = %q, want b5046 (empty pin file should fall through)", got)
	}
}

func TestResolver_FallsBackToCurrentFileWhenNoPin(t *testing.T) {
	home := t.TempDir()
	cwd := filepath.Join(home, "deep", "deeper")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	s := New(home)
	if err := s.SetActive("b5046"); err != nil {
		t.Fatal(err)
	}

	r := NewResolver(s, home)
	got, err := r.Resolve(cwd)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != "b5046" {
		t.Fatalf("Resolve = %q, want b5046", got)
	}
}

func TestResolver_NoPinNoCurrentReturnsErr(t *testing.T) {
	home := t.TempDir()
	cwd := filepath.Join(home, "project")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	r := NewResolver(New(home), home)
	if _, err := r.Resolve(cwd); !errors.Is(err, ErrNoActiveVersion) {
		t.Fatalf("Resolve: got %v, want ErrNoActiveVersion", err)
	}
}
```

- [ ] **Step 2: Run tests, verify they fail**

Run: `go test ./internal/version/ -run TestResolver -v`
Expected: FAIL — `NewResolver` signature is wrong (only one arg) and `Resolve` takes no args.

- [ ] **Step 3: Reimplement the resolver**

Replace `internal/version/resolver.go` with:

```go
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
// (\"\", false, nil) on no match, or an error on read failures other than
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
```

- [ ] **Step 4: Run tests, verify they pass**

Run: `go test ./internal/version/ -v`
Expected: PASS for the new resolver tests and all existing M2 store tests.

- [ ] **Step 5: Build to surface caller breakage**

Run: `go build ./...`
Expected: FAIL in `internal/cli/current.go` (calls `Resolve()` with no args), in `internal/cli/list_test.go` (`fakeResolver.Resolve()`), and in `cmd/llamavm/main.go` / `cmd/llamavm-shim/main.go` (`NewResolver(s)` missing the `home` arg). These are addressed in subsequent tasks; do not commit yet.

- [ ] **Step 6: Commit**

```bash
git add internal/version/resolver.go internal/version/resolver_test.go
git commit -m "feat(version): add cwd-walk for .llama-version with home boundary"
```

(The build is intentionally broken at this commit boundary because callers haven't migrated yet — Tasks 2–5 fix them. If you'd prefer one green commit, batch Tasks 1–5 together. The plan keeps them split for reviewability.)

---

## Task 2: Update `cli.Resolver` interface; add `Getwd` to Deps; update fakes

**Files:**
- Modify: `internal/cli/deps.go`
- Modify: `internal/cli/list_test.go`

- [ ] **Step 1: Update the `Resolver` interface and `Deps`**

In `internal/cli/deps.go`, change the `Resolver` interface:

```go
// Resolver returns the active tag for the given working directory or
// version.ErrNoActiveVersion. Pass "" when no cwd is available — the
// resolver will then consult only ~/.llamavm/current.
type Resolver interface {
	Resolve(cwd string) (string, error)
}
```

And extend `Deps` with a `Getwd` function (place it adjacent to `Now`):

```go
type Deps struct {
	Store         Store
	GitHub        GitHubClient
	Builder       Builder
	Git           CommandRunner
	Platform      Platform
	Resolver      Resolver
	ShimInstaller ShimInstaller
	Stdout        io.Writer
	Stderr        io.Writer
	Now           func() time.Time
	Getwd         func() (string, error)
}
```

- [ ] **Step 2: Update `fakeResolver`**

In `internal/cli/list_test.go`, replace the `fakeResolver` block with:

```go
// fakeResolver implements Resolver. lastCwd records what Resolve was called
// with so tests can assert that subcommands threaded cwd through correctly.
type fakeResolver struct {
	tag     string
	err     error
	lastCwd string
}

func (r *fakeResolver) Resolve(cwd string) (string, error) {
	r.lastCwd = cwd
	return r.tag, r.err
}
```

- [ ] **Step 3: Compile-check**

Run: `go build ./internal/cli/...`
Expected: FAIL only in `internal/cli/current.go` (still calls `Resolve()` with no args); other cli files build clean. The `cmd/...` binaries are still broken from Task 1 — that's fine.

- [ ] **Step 4: Verify existing cli tests still type-check**

Run: `go vet ./internal/cli/...`
Expected: type errors point only at `current.go` — confirms the new interface is wired everywhere except the call site we'll fix in Task 3.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/deps.go internal/cli/list_test.go
git commit -m "feat(cli): thread cwd through Resolver interface; add Deps.Getwd"
```

---

## Task 3: `current` subcommand uses cwd

PRD §3.8: `current` resolves the same way the shim does — `.llama-version` first, `~/.llamavm/current` second, error third.

**Files:**
- Modify: `internal/cli/current.go`
- Modify: `internal/cli/current_test.go`

- [ ] **Step 1: Replace the test file**

Overwrite `internal/cli/current_test.go` with:

```go
package cli

import (
	"errors"
	"strings"
	"testing"

	"github.com/gregmundy/llamavm/internal/version"
)

func TestCurrent_PrintsActiveTag(t *testing.T) {
	res := &fakeResolver{tag: "b5046"}
	deps := &Deps{
		Store:    &fakeStore{},
		Resolver: res,
		Getwd:    func() (string, error) { return "/work/project", nil },
	}
	out, _, err := runRoot(t, deps, "current")
	if err != nil {
		t.Fatalf("current: %v", err)
	}
	if out != "b5046\n" {
		t.Fatalf("stdout = %q, want \"b5046\\n\"", out)
	}
	if res.lastCwd != "/work/project" {
		t.Fatalf("Resolver.Resolve called with cwd=%q, want \"/work/project\"", res.lastCwd)
	}
}

func TestCurrent_NoActiveVersion(t *testing.T) {
	deps := &Deps{
		Store:    &fakeStore{},
		Resolver: &fakeResolver{err: version.ErrNoActiveVersion},
		Getwd:    func() (string, error) { return "/work/project", nil },
	}
	_, errOut, err := runRoot(t, deps, "current")
	if err == nil {
		t.Fatal("expected error when no active version")
	}
	if !errors.Is(err, ErrUserError) {
		t.Fatalf("err = %v, want chained ErrUserError", err)
	}
	combined := errOut + err.Error()
	if !strings.Contains(combined, "No active version") {
		t.Fatalf("output = %q, want it to mention 'No active version'", combined)
	}
}

func TestCurrent_PropagatesUnexpectedError(t *testing.T) {
	deps := &Deps{
		Store:    &fakeStore{},
		Resolver: &fakeResolver{err: errors.New("disk on fire")},
		Getwd:    func() (string, error) { return "/work/project", nil },
	}
	if _, _, err := runRoot(t, deps, "current"); err == nil {
		t.Fatal("expected error to propagate")
	}
}

func TestCurrent_GetwdErrorPropagates(t *testing.T) {
	deps := &Deps{
		Store:    &fakeStore{},
		Resolver: &fakeResolver{tag: "b5046"},
		Getwd:    func() (string, error) { return "", errors.New("getwd failed") },
	}
	_, _, err := runRoot(t, deps, "current")
	if err == nil {
		t.Fatal("expected error when Getwd fails")
	}
	if errors.Is(err, ErrUserError) {
		t.Fatalf("Getwd failure should not be a user error, got %v", err)
	}
}
```

- [ ] **Step 2: Run tests, verify they fail**

Run: `go test ./internal/cli/ -run TestCurrent -v`
Expected: FAIL — `current.go` still calls `Resolve()` with no args, and doesn't call `Getwd`.

- [ ] **Step 3: Update `current.go`**

Replace `internal/cli/current.go` with:

```go
package cli

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/gregmundy/llamavm/internal/version"
)

func newCurrentCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "current",
		Short: "Print the active llama.cpp version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runCurrent(deps)
		},
	}
}

func runCurrent(deps *Deps) error {
	cwd, err := deps.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}
	tag, err := deps.Resolver.Resolve(cwd)
	if err != nil {
		if errors.Is(err, version.ErrNoActiveVersion) {
			return fmt.Errorf("No active version: %w", ErrUserError)
		}
		return fmt.Errorf("resolve active version: %w", err)
	}
	fmt.Fprintln(deps.Stdout, tag)
	return nil
}
```

- [ ] **Step 4: Run tests, verify they pass**

Run: `go test ./internal/cli/ -run TestCurrent -v`
Expected: PASS for all four current tests.

- [ ] **Step 5: Run the whole cli package**

Run: `go test ./internal/cli/ -count=1`
Expected: PASS — no regressions in install/list/uninstall/use tests.

- [ ] **Step 6: Commit**

```bash
git add internal/cli/current.go internal/cli/current_test.go
git commit -m "feat(cli): current resolves via Getwd-supplied cwd"
```

---

## Task 4: `pin` subcommand

PRD §3.9: writes the version string to `.llama-version` in cwd; warns (not errors) when the tag isn't installed.

**Files:**
- Create: `internal/cli/pin.go`
- Create: `internal/cli/pin_test.go`
- Modify: `internal/cli/root.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/cli/pin_test.go`:

```go
package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// pinCwd is a small helper: returns a Getwd that points at a fresh temp dir,
// plus the dir itself.
func pinCwd(t *testing.T) (string, func() (string, error)) {
	t.Helper()
	dir := t.TempDir()
	return dir, func() (string, error) { return dir, nil }
}

func TestPin_RequiresVersionArg(t *testing.T) {
	_, getwd := pinCwd(t)
	deps := &Deps{Store: &fakeStore{}, Getwd: getwd}
	if _, _, err := runRoot(t, deps, "pin"); err == nil {
		t.Fatal("expected error when version arg missing")
	}
}

func TestPin_HappyPathWritesFile(t *testing.T) {
	cwd, getwd := pinCwd(t)
	store := &fakeStore{installed: []string{"b5046"}}
	deps := &Deps{Store: store, Getwd: getwd}

	out, errOut, err := runRoot(t, deps, "pin", "b5046")
	if err != nil {
		t.Fatalf("pin: %v", err)
	}
	body, readErr := os.ReadFile(filepath.Join(cwd, ".llama-version"))
	if readErr != nil {
		t.Fatalf("read .llama-version: %v", readErr)
	}
	if string(body) != "b5046\n" {
		t.Fatalf(".llama-version body = %q, want \"b5046\\n\"", string(body))
	}
	if !strings.Contains(out, "b5046") {
		t.Fatalf("stdout = %q, want it to mention b5046", out)
	}
	if errOut != "" {
		t.Fatalf("stderr = %q, want empty (no warning when installed)", errOut)
	}
}

func TestPin_OverwritesExistingFile(t *testing.T) {
	cwd, getwd := pinCwd(t)
	if err := os.WriteFile(filepath.Join(cwd, ".llama-version"), []byte("b9999\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := &fakeStore{installed: []string{"b5046"}}
	deps := &Deps{Store: store, Getwd: getwd}

	if _, _, err := runRoot(t, deps, "pin", "b5046"); err != nil {
		t.Fatalf("pin: %v", err)
	}
	body, _ := os.ReadFile(filepath.Join(cwd, ".llama-version"))
	if string(body) != "b5046\n" {
		t.Fatalf(".llama-version body = %q, want \"b5046\\n\"", string(body))
	}
}

func TestPin_NotInstalledWarnsAndStillWrites(t *testing.T) {
	cwd, getwd := pinCwd(t)
	store := &fakeStore{installed: []string{"b5489"}} // b5046 is NOT installed
	deps := &Deps{Store: store, Getwd: getwd}

	_, errOut, err := runRoot(t, deps, "pin", "b5046")
	if err != nil {
		t.Fatalf("pin: %v", err)
	}
	body, readErr := os.ReadFile(filepath.Join(cwd, ".llama-version"))
	if readErr != nil {
		t.Fatalf("expected file to be written even when tag is not installed: %v", readErr)
	}
	if string(body) != "b5046\n" {
		t.Fatalf(".llama-version body = %q, want \"b5046\\n\"", string(body))
	}
	if !strings.Contains(strings.ToLower(errOut), "warning") {
		t.Fatalf("stderr = %q, want it to contain a warning", errOut)
	}
	if !strings.Contains(errOut, "b5046") {
		t.Fatalf("stderr = %q, want it to mention b5046", errOut)
	}
	if !strings.Contains(errOut, "llamavm install") {
		t.Fatalf("stderr = %q, want remediation to mention 'llamavm install'", errOut)
	}
}

func TestPin_GetwdErrorPropagates(t *testing.T) {
	deps := &Deps{
		Store: &fakeStore{installed: []string{"b5046"}},
		Getwd: func() (string, error) { return "", errors.New("getwd failed") },
	}
	if _, _, err := runRoot(t, deps, "pin", "b5046"); err == nil {
		t.Fatal("expected error when Getwd fails")
	}
}

func TestPin_WriteFailureIsErrored(t *testing.T) {
	// Point Getwd at a non-writable target (a regular file, not a directory)
	// so the os.WriteFile under it will fail.
	parent := t.TempDir()
	notADir := filepath.Join(parent, "block")
	if err := os.WriteFile(notADir, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	deps := &Deps{
		Store: &fakeStore{installed: []string{"b5046"}},
		Getwd: func() (string, error) { return notADir, nil },
	}
	if _, _, err := runRoot(t, deps, "pin", "b5046"); err == nil {
		t.Fatal("expected error when target dir is not a directory")
	}
}
```

- [ ] **Step 2: Run tests, verify they fail**

Run: `go test ./internal/cli/ -run TestPin -v`
Expected: FAIL — `pin` is not registered.

- [ ] **Step 3: Implement the subcommand**

Create `internal/cli/pin.go`:

```go
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
```

- [ ] **Step 4: Register in root**

In `internal/cli/root.go`, add inside `NewRoot` after the `newUseCmd` registration:

```go
	root.AddCommand(newPinCmd(deps))
```

- [ ] **Step 5: Run tests, verify they pass**

Run: `go test ./internal/cli/ -run TestPin -v`
Expected: PASS for all six pin tests.

- [ ] **Step 6: Run full cli package**

Run: `go test ./internal/cli/ -count=1`
Expected: PASS — no regressions.

- [ ] **Step 7: Commit**

```bash
git add internal/cli/pin.go internal/cli/pin_test.go internal/cli/root.go
git commit -m "feat(cli): add pin subcommand for per-directory version pinning"
```

---

## Task 5: Wire production binaries; manual smoke

This is the only OS-touching step.

**Files:**
- Modify: `cmd/llamavm/main.go`
- Modify: `cmd/llamavm-shim/main.go`

- [ ] **Step 1: Update `cmd/llamavm/main.go`**

In the `main()` body, change the `version.NewResolver(store)` call to `version.NewResolver(store, home)`, and add `Getwd: os.Getwd` to the `Deps` literal.

After: line 46 currently reads `resolver := version.NewResolver(store)` — replace with:

```go
	resolver := version.NewResolver(store, home)
```

And inside the `&cli.Deps{...}` literal (line 49–60), append a final field:

```go
		Now:           time.Now,
		Getwd:         os.Getwd,
```

(I.e., insert `Getwd: os.Getwd,` immediately after `Now: time.Now,`.)

- [ ] **Step 2: Update `cmd/llamavm-shim/main.go`**

Replace the contents with:

```go
package main

import (
	"fmt"
	"os"
	"syscall"

	"github.com/gregmundy/llamavm/internal/shim"
	"github.com/gregmundy/llamavm/internal/version"
)

func main() {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "llamavm-shim: cannot resolve home directory:", err)
		os.Exit(shim.ExitNotFound)
	}
	store := version.New(home)
	resolver := version.NewResolver(store, home)

	code := shim.Run(shim.Options{
		Argv: os.Args,
		Resolver: func() (string, error) {
			// PRD §3.12.2: the shim must consult .llama-version in cwd
			// before falling back to ~/.llamavm/current. If Getwd fails
			// (extremely rare — deleted cwd, etc.) pass "" and let the
			// resolver fall back to the current file.
			cwd, _ := os.Getwd()
			return resolver.Resolve(cwd)
		},
		VersionsDir: store.VersionsDir(),
		Stderr:      os.Stderr,
		Env:         os.Environ(),
		ExecFn:      syscall.Exec,
	})
	os.Exit(code)
}
```

- [ ] **Step 3: Build everything**

Run: `go build ./...`
Expected: builds clean — both binaries link.

- [ ] **Step 4: Run all tests**

Run: `go test ./...`
Expected: everything passes.

- [ ] **Step 5: Static checks**

Run: `go vet ./...`
Expected: no findings.

If `staticcheck` is available locally:
Run: `staticcheck ./...`
Expected: no findings.

- [ ] **Step 6: Manual smoke (no llama.cpp install required)**

```bash
TMPHOME=$(mktemp -d)
mkdir -p "$TMPHOME/.llamavm/versions/b5046/bin"
mkdir -p "$TMPHOME/.llamavm/versions/b5489/bin"
mkdir -p "$TMPHOME/.llamavm/shims"

printf '#!/bin/sh\necho "version: 5046 (commit smoke)"\n' > "$TMPHOME/.llamavm/versions/b5046/bin/llama-cli"
chmod +x "$TMPHOME/.llamavm/versions/b5046/bin/llama-cli"
printf '#!/bin/sh\necho "version: 5489 (commit smoke)"\n' > "$TMPHOME/.llamavm/versions/b5489/bin/llama-cli"
chmod +x "$TMPHOME/.llamavm/versions/b5489/bin/llama-cli"

echo b5046 > "$TMPHOME/.llamavm/current"

go build -o "$TMPHOME/llamavm" ./cmd/llamavm
go build -o "$TMPHOME/llamavm-shim" ./cmd/llamavm-shim
cp "$TMPHOME/llamavm-shim" "$TMPHOME/.llamavm/shims/llama-cli"

# 1. current with no .llama-version → falls back to ~/.llamavm/current
HOME="$TMPHOME" "$TMPHOME/llamavm" current
# Expected: b5046

# 2. shim invocation (also: no .llama-version) → uses b5046
HOME="$TMPHOME" "$TMPHOME/.llamavm/shims/llama-cli"
# Expected: version: 5046 (commit smoke)

# 3. pin b5489 inside a project dir (b5489 IS installed → no warning)
PROJECT="$TMPHOME/work/proj"
mkdir -p "$PROJECT"
(cd "$PROJECT" && HOME="$TMPHOME" "$TMPHOME/llamavm" pin b5489)
# Expected stdout: "Pinned b5489 in $PROJECT/.llama-version"
# Expected stderr: empty

# 4. current inside the pinned dir
(cd "$PROJECT" && HOME="$TMPHOME" "$TMPHOME/llamavm" current)
# Expected: b5489

# 5. current outside the pinned dir falls back to ~/.llamavm/current
HOME="$TMPHOME" "$TMPHOME/llamavm" current
# Expected: b5046

# 6. shim invocation inside the pinned dir uses b5489
(cd "$PROJECT" && HOME="$TMPHOME" "$TMPHOME/.llamavm/shims/llama-cli")
# Expected: version: 5489 (commit smoke)

# 7. pin a tag that's NOT installed — file is still written, with a warning
NEWPROJECT="$TMPHOME/work/future"
mkdir -p "$NEWPROJECT"
(cd "$NEWPROJECT" && HOME="$TMPHOME" "$TMPHOME/llamavm" pin b9999)
# Expected: stderr contains "warning: b9999 is not currently installed";
#           stdout contains "Pinned b9999 in <NEWPROJECT>/.llama-version"
cat "$NEWPROJECT/.llama-version"
# Expected: b9999

# 8. trailing whitespace in .llama-version is tolerated
printf '   b5046   \n\n' > "$NEWPROJECT/.llama-version"
(cd "$NEWPROJECT" && HOME="$TMPHOME" "$TMPHOME/llamavm" current)
# Expected: b5046

# 9. ancestors are walked (.llama-version above the cwd)
DEEP="$PROJECT/a/b/c"
mkdir -p "$DEEP"
(cd "$DEEP" && HOME="$TMPHOME" "$TMPHOME/llamavm" current)
# Expected: b5489 (from $PROJECT/.llama-version)

# 10. home boundary is respected: drop a .llama-version above $TMPHOME
ABOVE_HOME="$TMPHOME/.."
echo b9999 > "$ABOVE_HOME/.llama-version"
(cd "$TMPHOME" && HOME="$TMPHOME" "$TMPHOME/llamavm" current)
# Expected: b5046 (NOT b9999 — walk stopped at home)
rm -f "$ABOVE_HOME/.llama-version"
```

Document any unexpected output before committing.

- [ ] **Step 7: Commit**

```bash
git add cmd/llamavm/main.go cmd/llamavm-shim/main.go
git commit -m "feat(cmd): wire cwd-aware Resolver and Getwd into both binaries"
```

---

## Self-Review Notes

**Spec coverage check (PRD §11.1 M3):**
- `.llama-version` cwd-walk in resolver → Task 1 (`Resolver.Resolve(cwd)` + `findInAncestors`)
- Cwd-walk in shim → Task 5 (`cmd/llamavm-shim/main.go` closure passes `os.Getwd()` into `resolver.Resolve`)
- Cwd-walk in `current` subcommand → Task 3 (`runCurrent` calls `deps.Getwd()` then `Resolver.Resolve(cwd)`)
- `pin` subcommand → Task 4
- Acceptance: pinning a version in a directory causes shims invoked from that directory to use the pinned version → Task 5 manual smoke step 6

**Spec details cross-checked against implementation:**
- §3.8 resolution order (`.llama-version` → `~/.llamavm/current` → error): Resolver in Task 1.
- §3.9 pin overwrite semantics: `os.Rename` overwrites; covered by `TestPin_OverwritesExistingFile`.
- §3.9 warn-but-write when not installed: `TestPin_NotInstalledWarnsAndStillWrites`.
- §3.12.2 shim resolution steps 1–6: step 1–2 (binary name, resolve tag) handled by existing `shim.Run`; step 2 now uses cwd via the Task 5 closure. Steps 3–6 already correct from M2 (no changes needed).
- §3.13 file format (single line, trim whitespace tolerated): `TestResolver_TrimsWhitespace`, `TestResolver_EmptyFileFallsThrough`.

**Out-of-scope check:**
- No `bench` (M4).
- No `doctor` / Homebrew / README / release (M5).
- No GitHub-API tag validation in `pin` — PRD §3.9 explicitly allows pinning a not-yet-installed tag.

**Type consistency check:**
- `Resolver.Resolve(cwd string) (string, error)` — same signature in `internal/version/resolver.go`, `internal/cli/deps.go` interface, `fakeResolver` in `list_test.go`, and the call sites in `current.go` and `cmd/llamavm-shim/main.go` (via closure).
- `version.NewResolver(*Store, string)` — same signature in `cmd/llamavm/main.go` and `cmd/llamavm-shim/main.go`.
- `Deps.Getwd func() (string, error)` — same signature where set (production: `os.Getwd`; tests: lambdas) and consumed (`runCurrent`, `runPin`).
- `version.PinFileName` — referenced from `pin.go`; defined in `resolver.go`.

**One-green-commit option:** Task 1's commit leaves `go build ./...` broken because callers haven't migrated. If you want only green commits, batch Tasks 1–5 into a single commit; otherwise, the per-task split is preferable for review.
