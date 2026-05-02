# llamavm M2 — shims + use + current Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement the version-switching mechanic. After M2: `llamavm install <tag>` also drops three shim binaries (`llama-cli`, `llama-server`, `llama-quantize`) into `~/.llamavm/shims/`; `llamavm use <tag>` updates `~/.llamavm/current`; `llamavm current` prints the active tag (no decoration); invoking `llama-cli` from the shims dir execs the active version's real binary.

**Architecture:** Three thin layers. (1) `internal/version/resolver.go` resolves the active tag — in M2 it just reads `~/.llamavm/current`; M3 will add the cwd-walk. (2) `internal/shim` owns shim execution (`Run`) and shim filesystem installation (`Installer`); both take dependency-injected functions for `os.Executable`, `syscall.Exec`, and source binary location so tests don't shell out or call `Exec`. (3) `internal/cli` gains `use` and `current` subcommands plus a `ShimInstaller` dep that the install flow calls after `PromoteStaging`. Real wiring lives in `cmd/llamavm/main.go` and `cmd/llamavm-shim/main.go`.

**Tech Stack:** Go 1.22+, `github.com/spf13/cobra` v1.10.2, stdlib only (`os`, `os/exec`, `syscall`, `path/filepath`, `runtime`). No new deps.

**Out of scope (deferred to later milestones):**
- `.llama-version` cwd-walk and `pin` subcommand → M3
- `bench` → M4
- `doctor`, Homebrew tap, README, release → M5

**Per the PRD §11.1 milestone breakdown, M2's resolver only consults `~/.llamavm/current`.** PRD §3.8 lists the cwd-walk as part of `current`'s resolution order, but §11.1 explicitly punts that (and shim cwd-walk) to M3. Do not add it here. The `Resolver` is shaped so M3 can extend it without changing call sites.

---

## File Structure

**Created:**
- `internal/version/resolver.go` — active-version resolver (M2 = `Store.Active()` passthrough)
- `internal/version/resolver_test.go`
- `internal/shim/runner.go` — `shim.Run(Options)` resolves and execs the target binary
- `internal/shim/runner_test.go`
- `internal/shim/installer.go` — `Installer.EnsureInstalled(shimsDir)` writes shim binaries
- `internal/shim/installer_test.go`
- `internal/cli/use.go` + `use_test.go`
- `internal/cli/current.go` + `current_test.go`

**Modified:**
- `internal/version/store.go` — add `ShimsDir()` accessor
- `internal/version/store_test.go` — assert `ShimsDir()` path
- `internal/cli/deps.go` — add `Resolver`, `ShimInstaller` interfaces; extend `Deps`
- `internal/cli/install.go` — call `deps.ShimInstaller.EnsureInstalled` after `PromoteStaging`
- `internal/cli/install_test.go` — fake `ShimInstaller`; assert it's called once on success and not called on failure
- `internal/cli/list_test.go` — `fakeStore` gains `ShimsDir()` (and a fake `Resolver`/`ShimInstaller` lives nearby for new tests)
- `internal/cli/root.go` — register `use` and `current` subcommands
- `cmd/llamavm/main.go` — wire `version.NewResolver`, `shim.Installer{Source: defaultShimSource}`
- `cmd/llamavm-shim/main.go` — replace stub with real wiring of `shim.Run`
- `internal/shim/shim.go` — delete (replaced by `runner.go` / `installer.go`)

**Layout produced on disk after `llamavm install b5046` post-M2:**

```
~/.llamavm/
├── versions/b5046/{source,bin}
├── shims/
│   ├── llama-cli       # copy of llamavm-shim binary
│   ├── llama-server    # copy of llamavm-shim binary
│   └── llama-quantize  # copy of llamavm-shim binary
└── current             # "b5046\n"
```

---

## Task 1: Add `ShimsDir()` to Store

**Files:**
- Modify: `internal/version/store.go`
- Modify: `internal/version/store_test.go`

- [ ] **Step 1: Extend the path test**

In `internal/version/store_test.go` add to `TestStore_PathsAndRoot` (after the existing `LogsDir` assertion):

```go
	if got, want := s.ShimsDir(), filepath.Join(home, ".llamavm", "shims"); got != want {
		t.Fatalf("ShimsDir = %q, want %q", got, want)
	}
```

- [ ] **Step 2: Run test, verify it fails**

Run: `go test ./internal/version/ -run TestStore_PathsAndRoot -v`
Expected: FAIL — undefined `ShimsDir`.

- [ ] **Step 3: Add the accessor**

In `internal/version/store.go`, after the `LogsDir` accessor:

```go
func (s *Store) ShimsDir() string    { return filepath.Join(s.Root(), "shims") }
```

- [ ] **Step 4: Run test, verify it passes**

Run: `go test ./internal/version/ -run TestStore_PathsAndRoot -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/version/store.go internal/version/store_test.go
git commit -m "feat(version): add Store.ShimsDir() path accessor"
```

---

## Task 2: Version Resolver

In M2 the resolver is a thin wrapper around `Store.Active`. Shaped this way so M3 can layer in the cwd-walk by changing one method body, not every call site.

**Files:**
- Create: `internal/version/resolver.go`
- Create: `internal/version/resolver_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/version/resolver_test.go`:

```go
package version

import (
	"errors"
	"testing"
)

func TestResolver_NoActiveVersion(t *testing.T) {
	r := NewResolver(New(t.TempDir()))
	if _, err := r.Resolve(); !errors.Is(err, ErrNoActiveVersion) {
		t.Fatalf("Resolve on empty: got %v, want ErrNoActiveVersion", err)
	}
}

func TestResolver_ReadsCurrentFile(t *testing.T) {
	s := New(t.TempDir())
	if err := s.SetActive("b5046"); err != nil {
		t.Fatal(err)
	}
	r := NewResolver(s)
	got, err := r.Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != "b5046" {
		t.Fatalf("Resolve = %q, want b5046", got)
	}
}
```

- [ ] **Step 2: Run tests, verify they fail**

Run: `go test ./internal/version/ -run TestResolver -v`
Expected: FAIL — undefined `NewResolver`.

- [ ] **Step 3: Write the resolver**

Create `internal/version/resolver.go`:

```go
package version

// Resolver answers "which tag should a shim invocation use?".
// In M2 it is a thin wrapper around Store.Active. M3 will layer in
// the cwd-walk for .llama-version files in front of the current-file
// fallback by changing this method body — call sites stay identical.
type Resolver struct {
	store *Store
}

// NewResolver wraps a Store. The store must be non-nil.
func NewResolver(s *Store) *Resolver {
	return &Resolver{store: s}
}

// Resolve returns the active tag or ErrNoActiveVersion.
func (r *Resolver) Resolve() (string, error) {
	return r.store.Active()
}
```

- [ ] **Step 4: Run tests, verify they pass**

Run: `go test ./internal/version/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/version/resolver.go internal/version/resolver_test.go
git commit -m "feat(version): add Resolver wrapping Store.Active"
```

---

## Task 3: Add Resolver + ShimInstaller to Deps; expand fakeStore

We thread the new contracts before adding subcommands so subsequent tasks can use them.

**Files:**
- Modify: `internal/cli/deps.go`
- Modify: `internal/cli/list_test.go` (extend `fakeStore` + add helpers)

- [ ] **Step 1: Extend `Deps`**

In `internal/cli/deps.go`, add after the `Platform` interface:

```go
// Resolver returns the active tag or version.ErrNoActiveVersion.
type Resolver interface {
	Resolve() (string, error)
}

// ShimInstaller writes the three shim binaries into the shims directory.
// Implementations must be idempotent: calling EnsureInstalled twice with
// the same shimsDir is a no-op the second time.
type ShimInstaller interface {
	EnsureInstalled(shimsDir string) error
}
```

Also extend the `Store` interface (in the same file) with the new `ShimsDir` method:

```go
type Store interface {
	IsInstalled(tag string) bool
	List() ([]string, error)
	Active() (string, error)
	SetActive(tag string) error
	ClearActive() error
	Remove(tag string) error
	VersionDir(tag string) string
	StagingDir(tag string) string
	PromoteStaging(tag string) error
	RemoveStaging(tag string) error
	LogsDir() string
	ShimsDir() string
}
```

And add the new fields to `Deps`:

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
}
```

- [ ] **Step 2: Extend `fakeStore`**

In `internal/cli/list_test.go`, add to the `fakeStore` struct:

```go
	shimsDir string
```

And add the method after `LogsDir`:

```go
func (s *fakeStore) ShimsDir() string {
	if s.shimsDir != "" {
		return s.shimsDir
	}
	return "/fake/shims"
}
```

- [ ] **Step 3: Add reusable fakes for the new interfaces**

Append to `internal/cli/list_test.go`:

```go
// fakeResolver implements Resolver.
type fakeResolver struct {
	tag string
	err error
}

func (r *fakeResolver) Resolve() (string, error) { return r.tag, r.err }

// fakeShimInstaller implements ShimInstaller; records each call.
type fakeShimInstaller struct {
	calls []string
	err   error
}

func (i *fakeShimInstaller) EnsureInstalled(shimsDir string) error {
	i.calls = append(i.calls, shimsDir)
	return i.err
}
```

- [ ] **Step 4: Compile-check**

Run: `go build ./...`
Expected: builds clean.

Run: `go test ./internal/cli/ -count=1`
Expected: existing tests still pass.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/deps.go internal/cli/list_test.go
git commit -m "feat(cli): extend Deps with Resolver and ShimInstaller"
```

---

## Task 4: `current` subcommand

PRD §3.8: prints the active version with no decoration; exits non-zero ("No active version") when nothing is set.

**Files:**
- Create: `internal/cli/current.go`
- Create: `internal/cli/current_test.go`
- Modify: `internal/cli/root.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/cli/current_test.go`:

```go
package cli

import (
	"errors"
	"strings"
	"testing"

	"github.com/gregmundy/llamavm/internal/version"
)

func TestCurrent_PrintsActiveTag(t *testing.T) {
	deps := &Deps{
		Store:    &fakeStore{},
		Resolver: &fakeResolver{tag: "b5046"},
	}
	out, _, err := runRoot(t, deps, "current")
	if err != nil {
		t.Fatalf("current: %v", err)
	}
	if out != "b5046\n" {
		t.Fatalf("stdout = %q, want \"b5046\\n\"", out)
	}
}

func TestCurrent_NoActiveVersion(t *testing.T) {
	deps := &Deps{
		Store:    &fakeStore{},
		Resolver: &fakeResolver{err: version.ErrNoActiveVersion},
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
	}
	if _, _, err := runRoot(t, deps, "current"); err == nil {
		t.Fatal("expected error to propagate")
	}
}
```

- [ ] **Step 2: Run tests, verify they fail**

Run: `go test ./internal/cli/ -run TestCurrent -v`
Expected: FAIL — `current` is not a registered command.

- [ ] **Step 3: Implement the subcommand**

Create `internal/cli/current.go`:

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
	tag, err := deps.Resolver.Resolve()
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

- [ ] **Step 4: Register in root**

In `internal/cli/root.go`, add inside `NewRoot` after the existing `AddCommand` calls:

```go
	root.AddCommand(newCurrentCmd(deps))
```

- [ ] **Step 5: Run tests, verify they pass**

Run: `go test ./internal/cli/ -run TestCurrent -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/cli/current.go internal/cli/current_test.go internal/cli/root.go
git commit -m "feat(cli): add current subcommand"
```

---

## Task 5: `use` subcommand

PRD §3.7: writes tag to `~/.llamavm/current`; non-zero with a remediation message if not installed.

**Files:**
- Create: `internal/cli/use.go`
- Create: `internal/cli/use_test.go`
- Modify: `internal/cli/root.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/cli/use_test.go`:

```go
package cli

import (
	"errors"
	"strings"
	"testing"
)

func TestUse_RequiresVersion(t *testing.T) {
	deps := &Deps{Store: &fakeStore{}}
	if _, _, err := runRoot(t, deps, "use"); err == nil {
		t.Fatal("expected error when version arg missing")
	}
}

func TestUse_HappyPath(t *testing.T) {
	store := &fakeStore{installed: []string{"b5046"}}
	deps := &Deps{Store: store}
	out, _, err := runRoot(t, deps, "use", "b5046")
	if err != nil {
		t.Fatalf("use: %v", err)
	}
	if !store.hasActive || store.active != "b5046" {
		t.Fatalf("active not set: hasActive=%v active=%q", store.hasActive, store.active)
	}
	if !strings.Contains(out, "b5046") {
		t.Fatalf("stdout = %q, want it to mention b5046", out)
	}
}

func TestUse_NotInstalled_IsUserError(t *testing.T) {
	store := &fakeStore{installed: []string{"b5489"}}
	deps := &Deps{Store: store}
	_, _, err := runRoot(t, deps, "use", "b5046")
	if err == nil {
		t.Fatal("expected error when version not installed")
	}
	if !errors.Is(err, ErrUserError) {
		t.Fatalf("err = %v, want chained ErrUserError", err)
	}
	if !strings.Contains(err.Error(), "llamavm install") {
		t.Fatalf("err = %v, want it to suggest 'llamavm install'", err)
	}
	if store.hasActive {
		t.Fatal("active should not be set when version is not installed")
	}
}
```

- [ ] **Step 2: Run tests, verify they fail**

Run: `go test ./internal/cli/ -run TestUse -v`
Expected: FAIL — `use` is not registered.

- [ ] **Step 3: Implement the subcommand**

Create `internal/cli/use.go`:

```go
package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newUseCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "use <version>",
		Short: "Set the global active llama.cpp version",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUse(deps, args[0])
		},
	}
}

func runUse(deps *Deps, tag string) error {
	if !deps.Store.IsInstalled(tag) {
		return fmt.Errorf("%s is not installed; run 'llamavm install %s' first: %w", tag, tag, ErrUserError)
	}
	if err := deps.Store.SetActive(tag); err != nil {
		return fmt.Errorf("set active: %w", err)
	}
	fmt.Fprintf(deps.Stdout, "Active version: %s\n", tag)
	return nil
}
```

- [ ] **Step 4: Register in root**

In `internal/cli/root.go`, add inside `NewRoot`:

```go
	root.AddCommand(newUseCmd(deps))
```

- [ ] **Step 5: Run tests, verify they pass**

Run: `go test ./internal/cli/ -run TestUse -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/cli/use.go internal/cli/use_test.go internal/cli/root.go
git commit -m "feat(cli): add use subcommand"
```

---

## Task 6: Shim runner (`shim.Run`)

A pure function with all OS interactions injected: resolver, versions dir, exec function, env, stderr. PRD §3.12.2 lists six steps; tests cover each branch.

**Files:**
- Delete: `internal/shim/shim.go` (the M1 stub)
- Create: `internal/shim/runner.go`
- Create: `internal/shim/runner_test.go`

- [ ] **Step 1: Delete the stub**

```bash
rm internal/shim/shim.go
```

- [ ] **Step 2: Write the failing tests**

Create `internal/shim/runner_test.go`:

```go
package shim

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type recordedExec struct {
	path string
	argv []string
	envv []string
}

func newFakeExec() (*recordedExec, func(string, []string, []string) error) {
	rec := &recordedExec{}
	return rec, func(p string, argv, envv []string) error {
		rec.path = p
		rec.argv = append([]string(nil), argv...)
		rec.envv = append([]string(nil), envv...)
		return nil
	}
}

// makeShimsTree creates a versions tree with one tag and the requested binary names
// under <root>/versions/<tag>/bin. Returns the versions dir.
func makeShimsTree(t *testing.T, tag string, names ...string) string {
	t.Helper()
	root := t.TempDir()
	binDir := filepath.Join(root, "versions", tag, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(binDir, n), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return filepath.Join(root, "versions")
}

func TestRun_HappyPath(t *testing.T) {
	versionsDir := makeShimsTree(t, "b5046", "llama-cli")
	rec, fakeExec := newFakeExec()
	shimPath := filepath.Join(t.TempDir(), "shims", "llama-cli")

	code := Run(Options{
		Argv:        []string{shimPath, "--version"},
		Resolver:    func() (string, error) { return "b5046", nil },
		VersionsDir: versionsDir,
		Stderr:      io.Discard,
		Env:         []string{"FOO=bar"},
		ExecFn:      fakeExec,
	})
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	want := filepath.Join(versionsDir, "b5046", "bin", "llama-cli")
	if rec.path != want {
		t.Fatalf("exec path = %q, want %q", rec.path, want)
	}
	if len(rec.argv) != 2 || rec.argv[0] != shimPath || rec.argv[1] != "--version" {
		t.Fatalf("exec argv = %v, want [%s --version]", rec.argv, shimPath)
	}
	if len(rec.envv) != 1 || rec.envv[0] != "FOO=bar" {
		t.Fatalf("exec env = %v, want [FOO=bar]", rec.envv)
	}
}

func TestRun_UsesArgv0Basename(t *testing.T) {
	versionsDir := makeShimsTree(t, "b5046", "llama-server")
	rec, fakeExec := newFakeExec()
	// argv[0] is a full shim path; the runner must derive the binary name
	// via filepath.Base.
	shimPath := filepath.Join(t.TempDir(), "shims", "llama-server")

	code := Run(Options{
		Argv:        []string{shimPath, "-m", "model.gguf"},
		Resolver:    func() (string, error) { return "b5046", nil },
		VersionsDir: versionsDir,
		Stderr:      io.Discard,
		Env:         nil,
		ExecFn:      fakeExec,
	})
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if filepath.Base(rec.path) != "llama-server" {
		t.Fatalf("target binary = %q, want llama-server", rec.path)
	}
}

func TestRun_NoActiveVersion_Exit127(t *testing.T) {
	var stderr bytes.Buffer
	code := Run(Options{
		Argv:        []string{"/shims/llama-cli"},
		Resolver:    func() (string, error) { return "", errors.New("no active version") },
		VersionsDir: t.TempDir(),
		Stderr:      &stderr,
		ExecFn:      func(string, []string, []string) error { t.Fatal("exec should not run"); return nil },
	})
	if code != 127 {
		t.Fatalf("exit code = %d, want 127", code)
	}
	if !strings.Contains(stderr.String(), "no active version") {
		t.Fatalf("stderr = %q, want it to mention 'no active version'", stderr.String())
	}
}

func TestRun_BinaryMissing_Exit127(t *testing.T) {
	versionsDir := makeShimsTree(t, "b5046") // no binaries
	var stderr bytes.Buffer
	code := Run(Options{
		Argv:        []string{"/shims/llama-cli"},
		Resolver:    func() (string, error) { return "b5046", nil },
		VersionsDir: versionsDir,
		Stderr:      &stderr,
		ExecFn:      func(string, []string, []string) error { t.Fatal("exec should not run"); return nil },
	})
	if code != 127 {
		t.Fatalf("exit code = %d, want 127", code)
	}
	if !strings.Contains(stderr.String(), "llama-cli") {
		t.Fatalf("stderr = %q, want it to mention 'llama-cli'", stderr.String())
	}
}

func TestRun_ExecFails_Exit127(t *testing.T) {
	versionsDir := makeShimsTree(t, "b5046", "llama-cli")
	var stderr bytes.Buffer
	code := Run(Options{
		Argv:        []string{"/shims/llama-cli"},
		Resolver:    func() (string, error) { return "b5046", nil },
		VersionsDir: versionsDir,
		Stderr:      &stderr,
		ExecFn:      func(string, []string, []string) error { return errors.New("exec broken") },
	})
	if code != 127 {
		t.Fatalf("exit code = %d, want 127", code)
	}
	if !strings.Contains(stderr.String(), "exec broken") {
		t.Fatalf("stderr = %q, want it to mention 'exec broken'", stderr.String())
	}
}
```

- [ ] **Step 3: Run tests, verify they fail**

Run: `go test ./internal/shim/ -v`
Expected: FAIL — `Run` and `Options` undefined.

- [ ] **Step 4: Implement the runner**

Create `internal/shim/runner.go`:

```go
// Package shim implements the llama-* shim binary entry point and the
// installer that drops shim copies into ~/.llamavm/shims/.
package shim

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Names is the canonical list of shim binary names llamavm manages.
// Adding a new shim in v2 requires only appending to this slice.
var Names = []string{"llama-cli", "llama-server", "llama-quantize"}

// ExitNotFound is the conventional exit code for "command not found"
// (PRD §6.4 and §3.12.2 require 127 for shim resolution failures).
const ExitNotFound = 127

// Options is the dependency surface for Run. All OS-touching behavior
// is injected so unit tests can run without exec, syscalls, or hard-coded
// home directories.
type Options struct {
	Argv        []string                                          // process argv (Argv[0] is the shim's own path)
	Resolver    func() (string, error)                            // returns active tag
	VersionsDir string                                            // <home>/.llamavm/versions
	Stderr      io.Writer                                         // where to write user-facing errors
	Env         []string                                          // environment to pass through
	ExecFn      func(path string, argv, envv []string) error      // syscall.Exec in production
}

// Run resolves the active version, finds the requested binary under
// VersionsDir, and execs it. Returns 0 on a successful exec setup
// (in production ExecFn does not return); returns ExitNotFound otherwise.
func Run(opts Options) int {
	if len(opts.Argv) == 0 {
		fmt.Fprintln(opts.Stderr, "llamavm: shim invoked with empty argv")
		return ExitNotFound
	}
	name := filepath.Base(opts.Argv[0])

	tag, err := opts.Resolver()
	if err != nil {
		fmt.Fprintf(opts.Stderr, "llamavm: %v\nrun 'llamavm install <version>' or 'llamavm use <version>'\n", err)
		return ExitNotFound
	}

	bin := filepath.Join(opts.VersionsDir, tag, "bin", name)
	if _, statErr := os.Stat(bin); statErr != nil {
		fmt.Fprintf(opts.Stderr, "llamavm: %s not found for active version %s (%v)\n", name, tag, statErr)
		return ExitNotFound
	}

	if err := opts.ExecFn(bin, opts.Argv, opts.Env); err != nil {
		fmt.Fprintf(opts.Stderr, "llamavm: exec %s failed: %v\n", bin, err)
		return ExitNotFound
	}
	return 0
}
```

- [ ] **Step 5: Run tests, verify they pass**

Run: `go test ./internal/shim/ -v`
Expected: PASS for all five Run tests.

- [ ] **Step 6: Commit**

```bash
git add internal/shim/runner.go internal/shim/runner_test.go
git rm internal/shim/shim.go
git commit -m "feat(shim): implement Run() with injectable Resolver, ExecFn"
```

---

## Task 7: Shim installer

Copies the shim source binary to `~/.llamavm/shims/<name>` for each name in `shim.Names`. Atomic per file (temp + rename). Idempotent. The source path is supplied by an injected `Source func() (string, error)` so tests don't need a real binary.

**Files:**
- Create: `internal/shim/installer.go`
- Create: `internal/shim/installer_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/shim/installer_test.go`:

```go
package shim

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// writeFakeShim writes a placeholder "binary" file and returns its path.
func writeFakeShim(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "llamavm-shim")
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestInstaller_WritesAllShims(t *testing.T) {
	src := writeFakeShim(t, "fake-shim-v1")
	shimsDir := filepath.Join(t.TempDir(), "shims")
	inst := &Installer{Source: func() (string, error) { return src, nil }}

	if err := inst.EnsureInstalled(shimsDir); err != nil {
		t.Fatalf("EnsureInstalled: %v", err)
	}
	for _, name := range Names {
		got, err := os.ReadFile(filepath.Join(shimsDir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if string(got) != "fake-shim-v1" {
			t.Fatalf("%s body = %q, want fake-shim-v1", name, string(got))
		}
		fi, err := os.Stat(filepath.Join(shimsDir, name))
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm()&0o100 == 0 {
			t.Fatalf("%s is not executable: mode=%v", name, fi.Mode())
		}
	}
}

func TestInstaller_IsIdempotent(t *testing.T) {
	src := writeFakeShim(t, "v1")
	shimsDir := filepath.Join(t.TempDir(), "shims")
	calls := 0
	inst := &Installer{Source: func() (string, error) {
		calls++
		return src, nil
	}}
	if err := inst.EnsureInstalled(shimsDir); err != nil {
		t.Fatal(err)
	}
	// Mutate one shim so we can detect unwanted overwrites.
	mutated := filepath.Join(shimsDir, "llama-cli")
	if err := os.WriteFile(mutated, []byte("user-modified"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := inst.EnsureInstalled(shimsDir); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(mutated)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "user-modified" {
		t.Fatalf("idempotent install overwrote existing shim: body = %q", string(got))
	}
	// Source func may be called once or twice depending on impl, but never zero.
	if calls == 0 {
		t.Fatal("Source was never called")
	}
}

func TestInstaller_SourceErrorPropagates(t *testing.T) {
	inst := &Installer{Source: func() (string, error) { return "", errors.New("no shim source") }}
	err := inst.EnsureInstalled(filepath.Join(t.TempDir(), "shims"))
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestInstaller_CreatesShimsDir(t *testing.T) {
	src := writeFakeShim(t, "v1")
	root := t.TempDir()
	shimsDir := filepath.Join(root, "deep", "shims")
	inst := &Installer{Source: func() (string, error) { return src, nil }}
	if err := inst.EnsureInstalled(shimsDir); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(shimsDir)
	if err != nil || !fi.IsDir() {
		t.Fatalf("expected shimsDir to be created, stat err=%v isDir=%v", err, fi != nil && fi.IsDir())
	}
}

// Sanity: confirm we use fs.ErrExist semantics, not crash, when shim already exists.
func TestInstaller_StatExistingIsNotAnError(t *testing.T) {
	src := writeFakeShim(t, "v1")
	shimsDir := filepath.Join(t.TempDir(), "shims")
	if err := os.MkdirAll(shimsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(shimsDir, "llama-cli"), []byte("pre-existing"), 0o755); err != nil {
		t.Fatal(err)
	}
	inst := &Installer{Source: func() (string, error) { return src, nil }}
	if err := inst.EnsureInstalled(shimsDir); err != nil {
		// Make sure we didn't trip on "file exists" semantics.
		if errors.Is(err, fs.ErrExist) {
			t.Fatalf("EnsureInstalled returned fs.ErrExist on pre-existing shim: %v", err)
		}
		t.Fatalf("EnsureInstalled: %v", err)
	}
}
```

- [ ] **Step 2: Run tests, verify they fail**

Run: `go test ./internal/shim/ -run TestInstaller -v`
Expected: FAIL — `Installer` undefined.

- [ ] **Step 3: Implement the installer**

Create `internal/shim/installer.go`:

```go
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
```

- [ ] **Step 4: Run tests, verify they pass**

Run: `go test ./internal/shim/ -v`
Expected: PASS for all installer tests + the runner tests from Task 6.

- [ ] **Step 5: Commit**

```bash
git add internal/shim/installer.go internal/shim/installer_test.go
git commit -m "feat(shim): add idempotent Installer that copies shim binary"
```

---

## Task 8: Wire shim install into the install flow

After `PromoteStaging` succeeds (so we don't litter shims when an install fails), call `deps.ShimInstaller.EnsureInstalled(deps.Store.ShimsDir())`.

**Files:**
- Modify: `internal/cli/install.go`
- Modify: `internal/cli/install_test.go`

- [ ] **Step 1: Add the test**

In `internal/cli/install_test.go`, update `newInstallDeps` so it always sets a non-nil `ShimInstaller` and returns it:

Replace the existing signature:
```go
func newInstallDeps(t *testing.T, store Store) (*Deps, *fakeGitHub, *fakeBuilder, *fakeCmdRunner)
```
with:
```go
func newInstallDeps(t *testing.T, store Store) (*Deps, *fakeGitHub, *fakeBuilder, *fakeCmdRunner, *fakeShimInstaller)
```

Inside the function, after `b := &fakeBuilder{}`, add:
```go
	si := &fakeShimInstaller{}
```

Add `ShimInstaller: si,` to the `&Deps{...}` literal, and change the return to `return deps, g, b, r, si`. Update each existing caller (`deps, _, _, _ := newInstallDeps(...)` etc.) to add a trailing `_` for the new return.

Then add a new test below `TestInstall_HappyPath`:

```go
func TestInstall_HappyPathInstallsShimsOnce(t *testing.T) {
	store := newRealPathStore(t)
	deps, _, _, _, si := newInstallDeps(t, store)
	if _, _, err := runRoot(t, deps, "install", "b5046"); err != nil {
		t.Fatalf("install: %v", err)
	}
	if len(si.calls) != 1 {
		t.Fatalf("ShimInstaller calls = %d, want 1", len(si.calls))
	}
	wantSuffix := filepath.Join(".llamavm", "shims")
	// realPathStore root is the temp home; ShimsDir() = root/.llamavm/shims —
	// but realPathStore has root = t.TempDir(), and ShimsDir is whatever the
	// fake returns. Just assert the path ends in /shims so we don't pin to
	// implementation details.
	if !strings.HasSuffix(si.calls[0], "shims") && !strings.HasSuffix(si.calls[0], wantSuffix) {
		t.Fatalf("ShimInstaller called with %q, want a shims dir", si.calls[0])
	}
}

func TestInstall_FailureSkipsShimInstall(t *testing.T) {
	store := newRealPathStore(t)
	deps, _, b, _, si := newInstallDeps(t, store)
	b.err = errors.New("cmake exited 1")
	_, _, err := runRoot(t, deps, "install", "b5046")
	if err == nil {
		t.Fatal("expected error")
	}
	if len(si.calls) != 0 {
		t.Fatalf("ShimInstaller called %d times on failed install, want 0", len(si.calls))
	}
}
```

The `realPathStore` needs `ShimsDir()` so install can pass it. Add to the `realPathStore` block (near the existing `RemoveStaging` method):

```go
func (s *realPathStore) ShimsDir() string {
	return filepath.Join(s.root, "shims")
}
```

- [ ] **Step 2: Run tests, verify they fail**

Run: `go test ./internal/cli/ -run TestInstall -v`
Expected: FAIL — install doesn't call the installer yet (and the fake returns 0 calls on a successful install).

- [ ] **Step 3: Wire the call into install**

In `internal/cli/install.go`, after `if err := deps.Store.PromoteStaging(tag); err != nil { ... }` and before the `Active` block, add:

```go
	if err := deps.ShimInstaller.EnsureInstalled(deps.Store.ShimsDir()); err != nil {
		return fmt.Errorf("install shims: %w", err)
	}
```

- [ ] **Step 4: Run tests, verify they pass**

Run: `go test ./internal/cli/ -v`
Expected: PASS — all install tests including the two new ones.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/install.go internal/cli/install_test.go
git commit -m "feat(cli): install shims into ~/.llamavm/shims after PromoteStaging"
```

---

## Task 9: Wire real implementations into both binaries

This is the only OS-touching step. Tests in earlier tasks already exercise the logic; here we wire production deps. The smoke check at the end is manual.

**Files:**
- Modify: `cmd/llamavm/main.go`
- Modify: `cmd/llamavm-shim/main.go`

- [ ] **Step 1: Update `cmd/llamavm/main.go`**

Replace the file contents:

```go
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/gregmundy/llamavm/internal/builder"
	"github.com/gregmundy/llamavm/internal/cli"
	gh "github.com/gregmundy/llamavm/internal/github"
	"github.com/gregmundy/llamavm/internal/shim"
	"github.com/gregmundy/llamavm/internal/version"
)

var llamavmVersion = "dev"

// defaultShimSource resolves the path of the llamavm-shim binary.
// Production layout (Homebrew, go install, GoReleaser) puts llamavm-shim
// next to llamavm in the same directory.
func defaultShimSource() (string, error) {
	self, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("locate llamavm executable: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(self)
	if err == nil {
		self = resolved
	}
	return filepath.Join(filepath.Dir(self), "llamavm-shim"), nil
}

func main() {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "llamavm: cannot resolve home directory:", err)
		os.Exit(1)
	}

	platform := builder.DefaultPlatform
	store := version.New(home)
	runner := builder.ExecRunner{}
	resolver := version.NewResolver(store)
	installer := &shim.Installer{Source: defaultShimSource}

	deps := &cli.Deps{
		Store:         store,
		GitHub:        gh.New(),
		Builder:       &builder.Builder{Runner: runner, Platform: platform},
		Git:           runner,
		Platform:      platform,
		Resolver:      resolver,
		ShimInstaller: installer,
		Stdout:        os.Stdout,
		Stderr:        os.Stderr,
		Now:           time.Now,
	}

	root := cli.NewRoot(deps, llamavmVersion)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := root.ExecuteContext(ctx); err != nil {
		if errors.Is(err, cli.ErrUserError) {
			os.Exit(2)
		}
		os.Exit(1)
	}
}
```

- [ ] **Step 2: Update `cmd/llamavm-shim/main.go`**

Replace contents:

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
	resolver := version.NewResolver(store)

	code := shim.Run(shim.Options{
		Argv:        os.Args,
		Resolver:    resolver.Resolve,
		VersionsDir: store.VersionsDir(),
		Stderr:      os.Stderr,
		Env:         os.Environ(),
		ExecFn:      syscall.Exec,
	})
	os.Exit(code)
}
```

Note: `store.VersionsDir()` is currently unexported through the `cli.Store` interface but defined on the concrete `*version.Store`. The shim main uses the concrete type directly, so this is fine.

- [ ] **Step 3: Build everything**

Run: `go build ./...`
Expected: builds clean.

- [ ] **Step 4: Run all tests one more time**

Run: `go test ./...`
Expected: everything passes.

- [ ] **Step 5: Run static checks**

Run: `go vet ./...`
Expected: no findings.

If `staticcheck` is available locally:
Run: `staticcheck ./...`
Expected: no findings.

- [ ] **Step 6: Manual smoke (no llama.cpp install required)**

In a scratch home directory:

```bash
TMPHOME=$(mktemp -d)
mkdir -p "$TMPHOME/.llamavm/versions/b5046/bin"
printf '#!/bin/sh\necho "version: 5046 (commit smoke)"\n' > "$TMPHOME/.llamavm/versions/b5046/bin/llama-cli"
chmod +x "$TMPHOME/.llamavm/versions/b5046/bin/llama-cli"
echo b5046 > "$TMPHOME/.llamavm/current"

go build -o "$TMPHOME/llamavm" ./cmd/llamavm
go build -o "$TMPHOME/llamavm-shim" ./cmd/llamavm-shim

# Place a shim by hand (install path requires real cmake; we exercise resolver+exec only).
ln -s "$TMPHOME/llamavm-shim" "$TMPHOME/.llamavm/shims/llama-cli" || cp "$TMPHOME/llamavm-shim" "$TMPHOME/.llamavm/shims/llama-cli"
mkdir -p "$TMPHOME/.llamavm/shims"
cp "$TMPHOME/llamavm-shim" "$TMPHOME/.llamavm/shims/llama-cli"

HOME="$TMPHOME" "$TMPHOME/llamavm" current
# Expected output: b5046

HOME="$TMPHOME" "$TMPHOME/.llamavm/shims/llama-cli" --version
# Expected: version: 5046 (commit smoke)

HOME="$TMPHOME" "$TMPHOME/llamavm" use b5489
# Expected: error mentioning "b5489 is not installed"

mkdir -p "$TMPHOME/.llamavm/versions/b5489"
HOME="$TMPHOME" "$TMPHOME/llamavm" use b5489
# Expected: "Active version: b5489"
HOME="$TMPHOME" "$TMPHOME/llamavm" current
# Expected: b5489
```

Document any unexpected output before committing.

- [ ] **Step 7: Commit**

```bash
git add cmd/llamavm/main.go cmd/llamavm-shim/main.go
git commit -m "feat(cmd): wire Resolver, ShimInstaller, and shim runner"
```

---

## Self-Review Notes

**Spec coverage check (PRD §11.1 M2):**
- Shim binary → Task 6 (`shim.Run`) + Task 9 (`cmd/llamavm-shim/main.go`)
- Shim installation during install → Task 7 (`Installer`) + Task 8 (wired into install flow)
- `~/.llamavm/current` file → already exists from M1 (`Store.SetActive`/`Store.Active`)
- `use` subcommand → Task 5
- `current` subcommand → Task 4
- Acceptance: `llama-cli --version` via shim reports active version → Task 9 manual smoke
- Acceptance: switching with `use` is reflected immediately → Task 5 + Task 9 manual smoke (no caching layer between `use` writing `current` and the next shim invocation reading `current`)

**Out-of-scope check:**
- No `.llama-version` cwd-walk anywhere (deferred to M3)
- No `pin` subcommand (deferred to M3)
- No `bench`, `doctor`, Homebrew (deferred to M4/M5)

**Type consistency check:**
- `shim.Names` is the slice referenced by both `Installer.EnsureInstalled` and the install flow's binaryNames is independent — those serve different purposes (linkable per-version vs. shim filesystem layout) and stay in their respective packages.
- `Resolver.Resolve` signature matches across `internal/version`, `internal/cli` (interface), and the function passed in `cmd/llamavm-shim/main.go`.
- `ShimInstaller.EnsureInstalled(string)` shape consistent across deps interface, fake, and real impl.
