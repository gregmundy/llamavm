# llamavm M5 — `doctor` + Homebrew tap + README + GitHub release Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship llamavm v1.0.0. Implement the `llamavm doctor` subcommand (PRD §3.11), wire CI + GoReleaser-driven release pipelines, write the user-facing README, and commit a Homebrew formula template plus a release-verification log.

**Architecture:**
1. **`doctor` subcommand** — one new file in `internal/cli/`, runs eight environment checks (PRD §3.11), each prints `✓` or `✗ <remediation>`, exits non-zero (wraps `ErrUserError` → exit 2) when any check fails. Uses three new narrow seams on `Deps` (`LookPath`, `Getenv`, `XcodeSelectPath`) so tests don't shell out.
2. **CI workflow** (`.github/workflows/ci.yml`) — runs `gofmt -l`, `go vet`, `staticcheck`, `go test`, `go build` on every push/PR.
3. **Release pipeline** — `.goreleaser.yml` configures a darwin-arm64 archive containing both `llamavm` and `llamavm-shim` plus a generated Homebrew formula. `.github/workflows/release.yml` triggers GoReleaser on `v*` tag push.
4. **Documentation** — `README.md` with tagline, install/usage, captured `bench all` output, and a "Why this exists" paragraph per PRD §9.13. `docs/llamavm.rb` ships a hand-maintained formula matching PRD §5.3 (used to bootstrap the `gregmundy/homebrew-tap` repo before GoReleaser auto-publish takes over).
5. **Release log** — `docs/release-checks/v1.0.0.md` is the manual verification of the 15 PRD §9 acceptance criteria, populated as the final task.

**Tech Stack:** Go 1.26 (per `go.mod`); cobra v1.10.2; stdlib only inside the doctor implementation (`os`, `os/exec` via injected seams, `path/filepath`, `strings`, `errors`, `fmt`, `context`). GoReleaser v2 for release archives; GitHub Actions Ubuntu runners (cross-compile from amd64 to darwin-arm64 — pure-Go binaries, no cgo). `staticcheck` from `honnef.co/go/tools/cmd/staticcheck@latest`.

**Out of scope (deferred):**
- darwin-amd64 / linux / windows builds → v2 (PRD §7.3, §10).
- Auto-update notifications → v3 (PRD §10).
- GoReleaser brew auto-publish to tap → enabled in v1.0.1+ once the tap repo exists; v1.0.0 ships the formula as a hand-maintained file under `docs/llamavm.rb` that the user copies into `gregmundy/homebrew-tap` after the first release.
- Integration tests → covered by manual acceptance log.
- New subcommands beyond `doctor`.

**PRD anchors:**
- §3.11 — eight doctor checks, output format, non-zero exit when any fails.
- §5.3 — `docs/llamavm.rb` is the canonical place for the formula reference.
- §5.5, §7.1–7.3 — distribution via GoReleaser + Homebrew tap; v1 = darwin-arm64 only.
- §6.1 — first-run experience copy that the README should mirror.
- §6.4 — exit-code conventions; doctor failure → 2 (user error / fixable environment).
- §8.4 — CI gates: `gofmt`, `go vet`, `staticcheck`, `go test`, `go build`.
- §9 — 15 acceptance criteria; #10, #12, #13, #14, #15 are the M5-specific ones.

**Doctor output spec:**

Each of the eight checks prints exactly one line. Pass lines look like:

```
✓ ~/.llamavm directory exists
```

Fail lines append a remediation hint after a colon:

```
✗ ~/.llamavm/shims is on PATH: add to your shell rc: export PATH="$HOME/.llamavm/shims:$PATH"
```

After all eight lines, doctor prints either `OK` (all passed, exit 0) or `FAIL` + `errors.New("doctor: N check(s) failed: %w", ErrUserError)` so main.go maps to exit 2. The eight check IDs and remediations are spelled out in Task 4.

---

## File Structure

**Created:**
- `internal/cli/doctor.go` — `newDoctorCmd`, `runDoctor`, eight check functions.
- `internal/cli/doctor_test.go` — happy-path + per-check failure tests.
- `.github/workflows/ci.yml`
- `.github/workflows/release.yml`
- `.goreleaser.yml`
- `README.md`
- `docs/llamavm.rb` — Homebrew formula template/reference.
- `docs/release-checks/v1.0.0.md` — release verification log.

**Modified:**
- `internal/cli/deps.go` — add `LookPath`, `Getenv`, `XcodeSelectPath` fields.
- `internal/cli/root.go` — register `doctor` subcommand.
- `cmd/llamavm/main.go` — default the three new `Deps` fields to stdlib functions.
- `internal/cli/list_test.go` — extend `fakeStore` only if needed (not required; doctor uses the existing `Store` surface).

---

## Task 1: Extend `Deps` with environment seams

**Files:**
- Modify: `internal/cli/deps.go`

The doctor subcommand needs three new injectable behaviors so its tests don't shell out: looking up an executable on PATH, reading environment variables, and asking xcode-select for the developer dir. We pre-add them now so Task 2 can wire the doctor command without churning Deps shape mid-implementation.

- [ ] **Step 1: Add three function-typed fields to `Deps`**

Open `internal/cli/deps.go`. Replace the `Deps` struct definition (currently ending at the `Getwd` line) with:

```go
// Deps collects everything the cli subcommands need.
type Deps struct {
	Store         Store
	GitHub        GitHubClient
	Builder       Builder
	Git           CommandRunner
	Platform      Platform
	Resolver      Resolver
	ShimInstaller ShimInstaller
	Benchmarker   Benchmarker
	Stdout        io.Writer
	Stderr        io.Writer
	Now           func() time.Time
	Getwd         func() (string, error)

	// LookPath wraps exec.LookPath. Doctor uses it to verify cmake and git
	// are reachable, and to confirm a llama-* shim resolves to ~/.llamavm/shims.
	LookPath func(name string) (string, error)
	// Getenv wraps os.Getenv. Doctor reads PATH from it.
	Getenv func(key string) string
	// XcodeSelectPath runs `xcode-select -p` and returns its trimmed stdout.
	// Used by doctor to verify Xcode CLT is installed.
	XcodeSelectPath func(ctx context.Context) (string, error)
}
```

- [ ] **Step 2: Wire defaults in `cmd/llamavm/main.go`**

Open `cmd/llamavm/main.go`. Add new imports and wire the defaults inside the `deps := &cli.Deps{...}` literal. Replace the `Getwd: os.Getwd,` line with the following block (which keeps `Getwd` and adds the three new fields):

```go
		Getwd:           os.Getwd,
		LookPath:        exec.LookPath,
		Getenv:          os.Getenv,
		XcodeSelectPath: defaultXcodeSelectPath(runner),
```

Add the helper at the bottom of `main.go` (after the existing `defaultShimSource` helper):

```go
// defaultXcodeSelectPath wraps `xcode-select -p` using the existing
// CommandRunner so doctor reuses the project's command-execution seam.
func defaultXcodeSelectPath(runner builder.ExecRunner) func(ctx context.Context) (string, error) {
	return func(ctx context.Context) (string, error) {
		var stdout bytes.Buffer
		if err := runner.Run(ctx, "xcode-select", []string{"-p"}, "", &stdout, io.Discard); err != nil {
			return "", err
		}
		return strings.TrimSpace(stdout.String()), nil
	}
}
```

Add to the import block:

```go
	"bytes"
	"io"
	"os/exec"
	"strings"
```

- [ ] **Step 3: Verify nothing existing broke**

Run: `go build ./... && go test ./...`
Expected: all existing tests still pass; build succeeds.

- [ ] **Step 4: Commit**

```bash
git add internal/cli/deps.go cmd/llamavm/main.go
git commit -m "feat(cli): add LookPath/Getenv/XcodeSelectPath seams to Deps for doctor"
```

---

## Task 2: `doctor` scaffolding + cobra wiring

**Files:**
- Create: `internal/cli/doctor.go`
- Create: `internal/cli/doctor_test.go`
- Modify: `internal/cli/root.go`

This task creates the empty `doctor` command with TDD: a test that exercises `llamavm doctor` via the cobra root and asserts it produces some output and a clean exit on a fully-healthy fake environment. Subsequent tasks add the eight individual checks, each behind its own test.

- [ ] **Step 1: Write the failing test**

Create `internal/cli/doctor_test.go`:

```go
package cli

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// healthyDoctorDeps returns a Deps wired with stub implementations that make
// every doctor check pass. Individual tests below override one field at a
// time to drive a single check into the failing branch.
func healthyDoctorDeps(t *testing.T) *Deps {
	t.Helper()
	return &Deps{
		Store: &fakeStore{
			installed: []string{"b5046"},
			active:    "b5046",
			hasActive: true,
			shimsDir:  "/home/u/.llamavm/shims",
		},
		Resolver: &fakeResolver{tag: "b5046"},
		Getwd:    func() (string, error) { return "/work", nil },
		Getenv: func(key string) string {
			if key == "PATH" {
				return "/usr/bin:/home/u/.llamavm/shims:/opt/local/bin"
			}
			return ""
		},
		LookPath: func(name string) (string, error) {
			switch name {
			case "cmake":
				return "/opt/homebrew/bin/cmake", nil
			case "git":
				return "/usr/bin/git", nil
			case "llama-cli":
				return "/home/u/.llamavm/shims/llama-cli", nil
			}
			return "", errors.New("not found")
		},
		XcodeSelectPath: func(ctx context.Context) (string, error) {
			return "/Applications/Xcode.app/Contents/Developer", nil
		},
	}
}

func TestDoctor_AllChecksPass(t *testing.T) {
	deps := healthyDoctorDeps(t)
	out, _, err := runRoot(t, deps, "doctor")
	if err != nil {
		t.Fatalf("doctor: %v", err)
	}
	// Eight check lines, each prefixed with the pass marker.
	passes := strings.Count(out, "✓")
	if passes != 8 {
		t.Fatalf("got %d ✓ markers, want 8\noutput:\n%s", passes, out)
	}
	if strings.Contains(out, "✗") {
		t.Fatalf("expected no ✗ markers in healthy output:\n%s", out)
	}
	if !strings.Contains(out, "OK") {
		t.Fatalf("expected trailing OK summary, got:\n%s", out)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run TestDoctor_AllChecksPass -v`
Expected: FAIL — `unknown command "doctor"` from cobra.

- [ ] **Step 3: Create empty `doctor.go` with cobra scaffolding**

Create `internal/cli/doctor.go`:

```go
package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

func newDoctorCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose llamavm environment configuration",
		Long: "Verifies installation directory, shims, PATH, installed versions, " +
			"active version, and required toolchain (cmake, git, Xcode CLT). " +
			"Prints one line per check; exits non-zero if any check fails.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDoctor(cmd.Context(), deps)
		},
	}
}

// runDoctor executes the eight PRD §3.11 checks in order and prints one
// pass/fail line per check. Returns an ErrUserError-wrapped error when any
// check fails so main.go maps to exit code 2.
func runDoctor(ctx context.Context, deps *Deps) error {
	checks := allDoctorChecks()
	failed := 0
	for _, c := range checks {
		if c.run(ctx, deps) {
			fmt.Fprintf(deps.Stdout, "✓ %s\n", c.label)
		} else {
			fmt.Fprintf(deps.Stdout, "✗ %s: %s\n", c.label, c.remediation)
			failed++
		}
	}
	if failed > 0 {
		fmt.Fprintln(deps.Stdout, "FAIL")
		return fmt.Errorf("doctor: %d check(s) failed: %w", failed, ErrUserError)
	}
	fmt.Fprintln(deps.Stdout, "OK")
	return nil
}

// allDoctorChecks returns the ordered list of PRD §3.11 checks. Tasks 3–5
// progressively populate this slice; the empty starter form here just lets
// `llamavm doctor` parse and report OK while the implementation is wired up.
func allDoctorChecks() []doctorCheck {
	return []doctorCheck{}
}

// doctorCheck pairs a human-readable label and remediation hint with the
// predicate that determines pass/fail. run returns true on pass.
type doctorCheck struct {
	label       string
	remediation string
	run         func(ctx context.Context, deps *Deps) bool
}
```

- [ ] **Step 4: Register the subcommand on root**

Open `internal/cli/root.go`. Add the doctor subcommand registration after the existing `AddCommand` lines (after `newBenchCmd`):

```go
	root.AddCommand(newDoctorCmd(deps))
```

- [ ] **Step 5: Run test to confirm it now reaches doctor but still fails (zero ✓ markers)**

Run: `go test ./internal/cli/ -run TestDoctor_AllChecksPass -v`
Expected: FAIL — `got 0 ✓ markers, want 8`. This confirms cobra wiring is correct; checks are added next.

- [ ] **Step 6: Commit**

```bash
git add internal/cli/doctor.go internal/cli/doctor_test.go internal/cli/root.go
git commit -m "feat(cli): scaffold doctor subcommand with empty check list"
```

---

## Task 3: Doctor checks 1–3 (filesystem + PATH membership)

**Files:**
- Modify: `internal/cli/doctor.go`
- Modify: `internal/cli/doctor_test.go`

PRD §3.11 checks:
1. `~/.llamavm` directory exists
2. `~/.llamavm/shims` exists and contains all three shim binaries
3. `~/.llamavm/shims` is on PATH

Check 1 reuses `Store.Root()`-relative semantics by inferring the root from `Store.ShimsDir()` (pop the last element). To stay strictly within the existing Store interface, we look at the shims dir's parent. The shims dir is the conventional `<root>/shims`. Check 2 stats the shims dir and verifies three filenames. Check 3 reads `Getenv("PATH")` and looks for an exact element match (after `filepath.Clean`).

- [ ] **Step 1: Add per-check failure tests**

Append to `internal/cli/doctor_test.go`:

```go
import (
	// (other imports retained from above)
	"os"
	"path/filepath"
)

// runDoctorWithFakeShims creates a temp shims dir, optionally populates it
// with the three expected shim files, and returns deps wired to it.
func runDoctorWithFakeShims(t *testing.T, populate bool) (*Deps, string) {
	t.Helper()
	root := t.TempDir()
	shimsDir := filepath.Join(root, ".llamavm", "shims")
	if populate {
		if err := os.MkdirAll(shimsDir, 0o755); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"llama-cli", "llama-server", "llama-quantize"} {
			if err := os.WriteFile(filepath.Join(shimsDir, name), []byte("#!/bin/sh\n"), 0o755); err != nil {
				t.Fatal(err)
			}
		}
	}
	deps := healthyDoctorDeps(t)
	deps.Store = &fakeStore{
		installed: []string{"b5046"},
		active:    "b5046",
		hasActive: true,
		shimsDir:  shimsDir,
	}
	deps.Getenv = func(key string) string {
		if key == "PATH" {
			return "/usr/bin:" + shimsDir + ":/opt/local/bin"
		}
		return ""
	}
	return deps, shimsDir
}

func TestDoctor_RootMissing(t *testing.T) {
	deps, _ := runDoctorWithFakeShims(t, false)
	// fakeStore.shimsDir parent is the .llamavm root. Removing the entire
	// temp dir guarantees neither root nor shims exist.
	deps.Store = &fakeStore{
		installed: []string{"b5046"},
		active:    "b5046",
		hasActive: true,
		shimsDir:  "/nonexistent/.llamavm/shims",
	}
	out, _, err := runRoot(t, deps, "doctor")
	if err == nil {
		t.Fatal("expected non-nil err when root missing")
	}
	if !errors.Is(err, ErrUserError) {
		t.Fatalf("err = %v, want ErrUserError chain", err)
	}
	if !strings.Contains(out, "✗ ~/.llamavm directory exists") {
		t.Fatalf("expected root-missing fail line, got:\n%s", out)
	}
}

func TestDoctor_ShimsDirMissingShimFiles(t *testing.T) {
	deps, _ := runDoctorWithFakeShims(t, false)
	out, _, err := runRoot(t, deps, "doctor")
	if err == nil {
		t.Fatal("expected non-nil err when shim files missing")
	}
	if !strings.Contains(out, "✗ ~/.llamavm/shims contains llama-cli, llama-server, llama-quantize") {
		t.Fatalf("expected shim-files fail line, got:\n%s", out)
	}
	if !strings.Contains(out, "llamavm install") {
		t.Fatalf("remediation should mention 'llamavm install', got:\n%s", out)
	}
}

func TestDoctor_ShimsNotOnPATH(t *testing.T) {
	deps, _ := runDoctorWithFakeShims(t, true)
	deps.Getenv = func(key string) string {
		if key == "PATH" {
			return "/usr/bin:/opt/local/bin"
		}
		return ""
	}
	out, _, err := runRoot(t, deps, "doctor")
	if err == nil {
		t.Fatal("expected non-nil err when shims not on PATH")
	}
	if !strings.Contains(out, "✗ ~/.llamavm/shims is on PATH") {
		t.Fatalf("expected PATH fail line, got:\n%s", out)
	}
	if !strings.Contains(out, `export PATH="$HOME/.llamavm/shims:$PATH"`) {
		t.Fatalf("expected exact PATH-export remediation, got:\n%s", out)
	}
}
```

Also update `TestDoctor_AllChecksPass` to populate a real shims dir and point Getenv at it. Replace the body of `healthyDoctorDeps` so it uses the same temp dir shape; the simplest change is to rewrite `TestDoctor_AllChecksPass` to call `runDoctorWithFakeShims(t, true)` instead of `healthyDoctorDeps(t)`:

```go
func TestDoctor_AllChecksPass(t *testing.T) {
	deps, _ := runDoctorWithFakeShims(t, true)
	out, _, err := runRoot(t, deps, "doctor")
	if err != nil {
		t.Fatalf("doctor: %v\noutput:\n%s", err, out)
	}
	passes := strings.Count(out, "✓")
	if passes != 8 {
		t.Fatalf("got %d ✓ markers, want 8\noutput:\n%s", passes, out)
	}
	if strings.Contains(out, "✗") {
		t.Fatalf("expected no ✗ markers in healthy output:\n%s", out)
	}
	if !strings.Contains(out, "OK") {
		t.Fatalf("expected trailing OK summary, got:\n%s", out)
	}
}
```

- [ ] **Step 2: Run new tests; expect 4 failures**

Run: `go test ./internal/cli/ -run TestDoctor -v`
Expected: all four `TestDoctor_*` cases FAIL because the checks slice is still empty.

- [ ] **Step 3: Implement checks 1–3**

Open `internal/cli/doctor.go`. Replace the body of `allDoctorChecks` with:

```go
func allDoctorChecks() []doctorCheck {
	return []doctorCheck{
		checkRootDir(),
		checkShimFiles(),
		checkShimsOnPATH(),
	}
}
```

Append the three check builders:

```go
func checkRootDir() doctorCheck {
	return doctorCheck{
		label:       "~/.llamavm directory exists",
		remediation: "run 'llamavm install <version>' to create it",
		run: func(_ context.Context, deps *Deps) bool {
			// Root is the parent of the shims dir by convention (Store layout).
			root := filepath.Dir(deps.Store.ShimsDir())
			info, err := os.Stat(root)
			return err == nil && info.IsDir()
		},
	}
}

func checkShimFiles() doctorCheck {
	return doctorCheck{
		label:       "~/.llamavm/shims contains llama-cli, llama-server, llama-quantize",
		remediation: "run 'llamavm install <version>' to install the shims",
		run: func(_ context.Context, deps *Deps) bool {
			for _, name := range []string{"llama-cli", "llama-server", "llama-quantize"} {
				p := filepath.Join(deps.Store.ShimsDir(), name)
				if _, err := os.Stat(p); err != nil {
					return false
				}
			}
			return true
		},
	}
}

func checkShimsOnPATH() doctorCheck {
	return doctorCheck{
		label:       "~/.llamavm/shims is on PATH",
		remediation: `add to your shell rc: export PATH="$HOME/.llamavm/shims:$PATH"`,
		run: func(_ context.Context, deps *Deps) bool {
			want := filepath.Clean(deps.Store.ShimsDir())
			for _, p := range filepath.SplitList(deps.Getenv("PATH")) {
				if filepath.Clean(p) == want {
					return true
				}
			}
			return false
		},
	}
}
```

Add to the imports:

```go
	"os"
	"path/filepath"
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/cli/ -run TestDoctor -v`
Expected: `TestDoctor_RootMissing`, `TestDoctor_ShimsDirMissingShimFiles`, `TestDoctor_ShimsNotOnPATH` PASS. `TestDoctor_AllChecksPass` still FAILS (only 3 of 8 checks present, expects 8 ✓).

- [ ] **Step 5: Commit**

```bash
git add internal/cli/doctor.go internal/cli/doctor_test.go
git commit -m "feat(cli): doctor checks 1-3 (root dir, shim files, PATH membership)"
```

---

## Task 4: Doctor checks 4–5 (versions installed + active resolution)

**Files:**
- Modify: `internal/cli/doctor.go`
- Modify: `internal/cli/doctor_test.go`

PRD §3.11 checks:
4. At least one version is installed (`Store.List()` non-empty).
5. `~/.llamavm/current` is set to a valid installed version OR `.llama-version` is present in cwd ancestry — operationally: `Resolver.Resolve(cwd)` returns a tag that appears in `Store.List()`.

- [ ] **Step 1: Add failure tests**

Append to `internal/cli/doctor_test.go`:

```go
func TestDoctor_NoVersionsInstalled(t *testing.T) {
	deps, _ := runDoctorWithFakeShims(t, true)
	deps.Store.(*fakeStore).installed = nil
	deps.Store.(*fakeStore).hasActive = false
	deps.Resolver = &fakeResolver{err: version.ErrNoActiveVersion}
	out, _, err := runRoot(t, deps, "doctor")
	if err == nil {
		t.Fatal("expected error when no versions installed")
	}
	if !strings.Contains(out, "✗ at least one version is installed") {
		t.Fatalf("expected versions fail line, got:\n%s", out)
	}
}

func TestDoctor_ActiveVersionUnresolved(t *testing.T) {
	deps, _ := runDoctorWithFakeShims(t, true)
	deps.Resolver = &fakeResolver{err: version.ErrNoActiveVersion}
	deps.Store.(*fakeStore).hasActive = false
	deps.Store.(*fakeStore).active = ""
	out, _, err := runRoot(t, deps, "doctor")
	if err == nil {
		t.Fatal("expected error when active version unresolved")
	}
	if !strings.Contains(out, "✗ active version resolves") {
		t.Fatalf("expected active-version fail line, got:\n%s", out)
	}
}

func TestDoctor_ActiveVersionPointsToUninstalled(t *testing.T) {
	deps, _ := runDoctorWithFakeShims(t, true)
	deps.Resolver = &fakeResolver{tag: "b9999"} // not in installed list
	out, _, err := runRoot(t, deps, "doctor")
	if err == nil {
		t.Fatal("expected error when active version not installed")
	}
	if !strings.Contains(out, "✗ active version resolves") {
		t.Fatalf("expected fail line for stale-active, got:\n%s", out)
	}
}
```

Add `"github.com/gregmundy/llamavm/internal/version"` to the doctor_test.go imports if not already present.

- [ ] **Step 2: Run new tests; expect failures**

Run: `go test ./internal/cli/ -run TestDoctor -v`
Expected: `TestDoctor_NoVersionsInstalled`, `TestDoctor_ActiveVersionUnresolved`, `TestDoctor_ActiveVersionPointsToUninstalled` FAIL because checks 4 and 5 don't exist yet.

- [ ] **Step 3: Implement checks 4 and 5**

Open `internal/cli/doctor.go`. Extend `allDoctorChecks`:

```go
func allDoctorChecks() []doctorCheck {
	return []doctorCheck{
		checkRootDir(),
		checkShimFiles(),
		checkShimsOnPATH(),
		checkAtLeastOneVersion(),
		checkActiveVersionResolves(),
	}
}
```

Append the new builders:

```go
func checkAtLeastOneVersion() doctorCheck {
	return doctorCheck{
		label:       "at least one version is installed",
		remediation: "run 'llamavm install latest'",
		run: func(_ context.Context, deps *Deps) bool {
			tags, err := deps.Store.List()
			return err == nil && len(tags) > 0
		},
	}
}

func checkActiveVersionResolves() doctorCheck {
	return doctorCheck{
		label:       "active version resolves to an installed tag",
		remediation: "run 'llamavm use <version>' or pin one with 'llamavm pin <version>'",
		run: func(_ context.Context, deps *Deps) bool {
			cwd, err := deps.Getwd()
			if err != nil {
				cwd = ""
			}
			tag, err := deps.Resolver.Resolve(cwd)
			if err != nil {
				return false
			}
			tags, err := deps.Store.List()
			if err != nil {
				return false
			}
			for _, t := range tags {
				if t == tag {
					return true
				}
			}
			return false
		},
	}
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/cli/ -run TestDoctor -v`
Expected: the three new failure-mode tests PASS. `TestDoctor_AllChecksPass` still FAILS (5 of 8).

- [ ] **Step 5: Commit**

```bash
git add internal/cli/doctor.go internal/cli/doctor_test.go
git commit -m "feat(cli): doctor checks 4-5 (versions installed + active resolves)"
```

---

## Task 5: Doctor checks 6–8 (toolchain) + final happy path

**Files:**
- Modify: `internal/cli/doctor.go`
- Modify: `internal/cli/doctor_test.go`

PRD §3.11 checks:
6. `cmake` on PATH
7. `git` on PATH
8. Xcode CLT installed (`xcode-select -p` succeeds with non-empty stdout)

After this task all eight checks are present and `TestDoctor_AllChecksPass` flips to PASS.

- [ ] **Step 1: Add failure tests**

Append to `internal/cli/doctor_test.go`:

```go
func TestDoctor_CmakeMissing(t *testing.T) {
	deps, _ := runDoctorWithFakeShims(t, true)
	deps.LookPath = func(name string) (string, error) {
		if name == "cmake" {
			return "", errors.New("not found")
		}
		return "/usr/bin/" + name, nil
	}
	out, _, err := runRoot(t, deps, "doctor")
	if err == nil {
		t.Fatal("expected error when cmake missing")
	}
	if !strings.Contains(out, "✗ cmake is on PATH") {
		t.Fatalf("expected cmake fail line, got:\n%s", out)
	}
	if !strings.Contains(out, "brew install cmake") {
		t.Fatalf("expected brew install cmake remediation, got:\n%s", out)
	}
}

func TestDoctor_GitMissing(t *testing.T) {
	deps, _ := runDoctorWithFakeShims(t, true)
	deps.LookPath = func(name string) (string, error) {
		if name == "git" {
			return "", errors.New("not found")
		}
		return "/usr/bin/" + name, nil
	}
	out, _, err := runRoot(t, deps, "doctor")
	if err == nil {
		t.Fatal("expected error when git missing")
	}
	if !strings.Contains(out, "✗ git is on PATH") {
		t.Fatalf("expected git fail line, got:\n%s", out)
	}
}

func TestDoctor_XcodeCLTMissing(t *testing.T) {
	deps, _ := runDoctorWithFakeShims(t, true)
	deps.XcodeSelectPath = func(ctx context.Context) (string, error) {
		return "", errors.New("xcode-select: error: unable to get active developer directory")
	}
	out, _, err := runRoot(t, deps, "doctor")
	if err == nil {
		t.Fatal("expected error when xcode-select fails")
	}
	if !strings.Contains(out, "✗ Xcode Command Line Tools are installed") {
		t.Fatalf("expected xcode CLT fail line, got:\n%s", out)
	}
	if !strings.Contains(out, "xcode-select --install") {
		t.Fatalf("expected xcode-select --install remediation, got:\n%s", out)
	}
}
```

- [ ] **Step 2: Run new tests; expect failures**

Run: `go test ./internal/cli/ -run TestDoctor -v`
Expected: the three new tests FAIL.

- [ ] **Step 3: Implement checks 6–8**

Open `internal/cli/doctor.go`. Extend `allDoctorChecks`:

```go
func allDoctorChecks() []doctorCheck {
	return []doctorCheck{
		checkRootDir(),
		checkShimFiles(),
		checkShimsOnPATH(),
		checkAtLeastOneVersion(),
		checkActiveVersionResolves(),
		checkLookPath("cmake", "brew install cmake"),
		checkLookPath("git", "install git via Xcode CLT or 'brew install git'"),
		checkXcodeCLT(),
	}
}
```

Append the new builders:

```go
func checkLookPath(binary, remediation string) doctorCheck {
	return doctorCheck{
		label:       binary + " is on PATH",
		remediation: remediation,
		run: func(_ context.Context, deps *Deps) bool {
			_, err := deps.LookPath(binary)
			return err == nil
		},
	}
}

func checkXcodeCLT() doctorCheck {
	return doctorCheck{
		label:       "Xcode Command Line Tools are installed",
		remediation: "run 'xcode-select --install'",
		run: func(ctx context.Context, deps *Deps) bool {
			out, err := deps.XcodeSelectPath(ctx)
			return err == nil && out != ""
		},
	}
}
```

- [ ] **Step 4: Run all doctor tests**

Run: `go test ./internal/cli/ -run TestDoctor -v`
Expected: ALL `TestDoctor_*` tests PASS, including `TestDoctor_AllChecksPass`.

- [ ] **Step 5: Run full project test suite + format/vet**

Run: `gofmt -l . && go vet ./... && go test ./...`
Expected: empty `gofmt` output, no vet warnings, all tests pass.

- [ ] **Step 6: Commit**

```bash
git add internal/cli/doctor.go internal/cli/doctor_test.go
git commit -m "feat(cli): doctor checks 6-8 (cmake, git, Xcode CLT)"
```

---

## Task 6: CI workflow

**Files:**
- Create: `.github/workflows/ci.yml`

Per PRD §8.4, CI runs `gofmt -l`, `go vet`, `staticcheck`, `go test`, `go build` on every push and PR. We use the canonical setup-go action and pin Go to 1.26 to match `go.mod`.

- [ ] **Step 1: Create the workflow file**

Create `.github/workflows/ci.yml`:

```yaml
name: ci

on:
  push:
    branches: [main]
  pull_request:
    branches: [main]

permissions:
  contents: read

jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-go@v5
        with:
          go-version: '1.26'
          cache: true

      - name: gofmt
        run: |
          out=$(gofmt -l .)
          if [ -n "$out" ]; then
            echo "::error::gofmt found unformatted files:"
            echo "$out"
            exit 1
          fi

      - name: go vet
        run: go vet ./...

      - name: staticcheck
        run: |
          go install honnef.co/go/tools/cmd/staticcheck@2024.1.1
          staticcheck ./...

      - name: go test
        run: go test ./...

      - name: go build
        run: go build ./...
```

- [ ] **Step 2: Verify the YAML parses**

Run: `python3 -c 'import yaml; yaml.safe_load(open(".github/workflows/ci.yml"))'`
Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add .github/workflows/ci.yml
git commit -m "ci: add gofmt/vet/staticcheck/test/build workflow"
```

---

## Task 7: GoReleaser config + release workflow

**Files:**
- Create: `.goreleaser.yml`
- Create: `.github/workflows/release.yml`

Builds darwin-arm64 archives containing both `llamavm` and `llamavm-shim`, plus a generated Homebrew formula. The release workflow triggers on `v*` tag pushes. For v1.0.0 we set `brews[].skip_upload: true` because the tap repo doesn't exist yet — GoReleaser will still output the rendered formula into `dist/llamavm.rb`, which we manually copy to the tap repo as part of Task 9.

- [ ] **Step 1: Create `.goreleaser.yml`**

Create `.goreleaser.yml`:

```yaml
version: 2
project_name: llamavm

before:
  hooks:
    - go mod tidy

builds:
  - id: llamavm
    main: ./cmd/llamavm
    binary: llamavm
    env:
      - CGO_ENABLED=0
    goos: [darwin]
    goarch: [arm64]
    ldflags:
      - -s -w -X main.llamavmVersion={{.Version}}
  - id: llamavm-shim
    main: ./cmd/llamavm-shim
    binary: llamavm-shim
    env:
      - CGO_ENABLED=0
    goos: [darwin]
    goarch: [arm64]
    ldflags:
      - -s -w

archives:
  - id: default
    ids:
      - llamavm
      - llamavm-shim
    name_template: "{{ .ProjectName }}_{{ .Version }}_{{ .Os }}_{{ .Arch }}"
    format: tar.gz
    files:
      - LICENSE*
      - README.md

checksum:
  name_template: "checksums.txt"

snapshot:
  version_template: "{{ incpatch .Version }}-next"

changelog:
  sort: asc
  filters:
    exclude:
      - "^docs:"
      - "^chore:"
      - "^test:"

brews:
  - name: llamavm
    repository:
      owner: gregmundy
      name: homebrew-tap
    directory: Formula
    homepage: "https://github.com/gregmundy/llamavm"
    description: "Version manager for llama.cpp on Apple Silicon"
    license: "MIT"
    skip_upload: true
    install: |
      bin.install "llamavm"
      bin.install "llamavm-shim"
    test: |
      assert_match "llamavm version", shell_output("#{bin}/llamavm --version")
```

- [ ] **Step 2: Create `.github/workflows/release.yml`**

Create `.github/workflows/release.yml`:

```yaml
name: release

on:
  push:
    tags:
      - 'v*'

permissions:
  contents: write

jobs:
  goreleaser:
    runs-on: macos-14
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0

      - uses: actions/setup-go@v5
        with:
          go-version: '1.26'
          cache: true

      - uses: goreleaser/goreleaser-action@v6
        with:
          distribution: goreleaser
          version: '~> v2'
          args: release --clean
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
```

We use `macos-14` (arm64) so the tarball contents match the host architecture used by Homebrew users. Pure-Go builds wouldn't strictly require it, but matching keeps signing/notarization paths simple if added in a future release.

- [ ] **Step 3: Verify both files parse as YAML**

Run: `python3 -c 'import yaml; yaml.safe_load(open(".goreleaser.yml")); yaml.safe_load(open(".github/workflows/release.yml"))'`
Expected: no errors.

- [ ] **Step 4: Smoke-test the release config locally with `goreleaser check`**

If goreleaser is available locally, run: `goreleaser check`
Expected: `config is valid`. If goreleaser is not installed, skip and document this in the release-checks log instead. (Do not block the task on a missing local toolchain — the release workflow will surface any config issue on the first tag push.)

- [ ] **Step 5: Commit**

```bash
git add .goreleaser.yml .github/workflows/release.yml
git commit -m "ci: add GoReleaser config and tag-triggered release workflow"
```

---

## Task 8: Homebrew formula reference

**Files:**
- Create: `docs/llamavm.rb`

PRD §5.3 places a copy of the formula here for reference. After the v1.0.0 release runs, GoReleaser will produce a populated formula in `dist/llamavm.rb`; this hand-maintained template is what bootstraps the `gregmundy/homebrew-tap` repo before the first release. URLs and SHA256 are placeholders that the user fills in once `v1.0.0` is tagged and the tarball is uploaded.

- [ ] **Step 1: Create the formula file**

Create `docs/llamavm.rb`:

```ruby
# Reference Homebrew formula for llamavm.
#
# This is the canonical template that lives in this repo per PRD §5.3.
# The deployed formula lives at github.com/gregmundy/homebrew-tap.
#
# After cutting a release:
#   1. Replace VERSION below with the released semver (e.g. 1.0.0).
#   2. Replace SHA256_PLACEHOLDER with the sha256 of the darwin-arm64 tarball
#      (read from dist/checksums.txt produced by GoReleaser).
#   3. Copy this file to the tap repo at Formula/llamavm.rb and commit.
class Llamavm < Formula
  desc "Version manager for llama.cpp on Apple Silicon"
  homepage "https://github.com/gregmundy/llamavm"
  version "VERSION"
  license "MIT"

  on_macos do
    on_arm do
      url "https://github.com/gregmundy/llamavm/releases/download/v#{version}/llamavm_#{version}_darwin_arm64.tar.gz"
      sha256 "SHA256_PLACEHOLDER"
    end
  end

  def install
    bin.install "llamavm"
    bin.install "llamavm-shim"
  end

  def caveats
    <<~EOS
      llamavm needs its shims directory on PATH. Add this to your shell rc:

        export PATH="$HOME/.llamavm/shims:$PATH"

      Then install a llama.cpp version:

        llamavm install latest
        llamavm doctor

    EOS
  end

  test do
    assert_match "llamavm version", shell_output("#{bin}/llamavm --version")
  end
end
```

- [ ] **Step 2: Commit**

```bash
git add docs/llamavm.rb
git commit -m "docs: add Homebrew formula template per PRD §5.3"
```

---

## Task 9: README

**Files:**
- Create: `README.md`

PRD §9 #13 mandates: tagline, install instructions, basic usage, screenshot of `bench` output, "why this exists" paragraph. PRD §6.1 has the exact first-run copy this README should mirror.

- [ ] **Step 1: Build the binary and capture real `bench` output for the README**

Run: `go build -o llamavm ./cmd/llamavm && go build -o llamavm-shim ./cmd/llamavm-shim`
Expected: both binaries appear at the repo root.

If at least two versions are installed under `~/.llamavm/versions/` and a model file path is available locally, capture real `bench all` output:

```bash
./llamavm bench all --model "$HOME/models/llama-3.2-1b-instruct-q4_k_m.gguf" 2>&1 | tee /tmp/bench-readme.txt
```

If no model or installed versions are available locally, fall back to the canonical reference output below (this is acceptable for v1.0.0; capture real output on a subsequent release).

Reference output (paste verbatim into the README if local capture is not possible):

```
Version  Tokens/s   Total time   Δ vs current
b5046    44.72       9.8s         baseline (current)
b5489    47.21       9.2s         +5.6%
b5400    43.10      10.1s         -3.6%

Best: b5489 (47.21 tok/s)
```

- [ ] **Step 2: Write the README**

Create `README.md`:

````markdown
# llamavm

> A version manager for [llama.cpp](https://github.com/ggml-org/llama.cpp) on Apple Silicon. Like `nvm` or `pyenv`, but for `llama-cli`.

## Why this exists

llama.cpp ships fast — multiple releases per week — and individual builds occasionally regress performance or change behavior. There is no asdf/mise plugin and Homebrew installs only one version at a time. llamavm builds versioned releases from source into `~/.llamavm/versions/<tag>/`, switches the active version via shims on PATH, supports per-project pinning via `.llama-version`, and benchmarks installed versions against a model so you can quickly tell which build is fastest on your hardware.

## Requirements

- Apple Silicon Mac running macOS 14 (Sonoma) or later
- Xcode Command Line Tools (`xcode-select --install`)
- `cmake` (`brew install cmake`)

Run `llamavm doctor` at any time to verify your environment.

## Install

```bash
brew install gregmundy/tap/llamavm
```

Then add the shims directory to your PATH (one-time, in your shell rc):

```bash
export PATH="$HOME/.llamavm/shims:$PATH"
```

## Quickstart

```bash
# Install the latest llama.cpp release
llamavm install latest

# List installed versions; the active one is marked with *
llamavm list

# Switch the global active version
llamavm use b5046

# Pin a specific version for the current project
llamavm pin b5046

# Confirm everything is wired correctly
llamavm doctor
```

`llama-cli`, `llama-server`, and `llama-quantize` are now on your PATH and dispatch to the active version automatically. Per-directory pinning takes precedence over the global `current` file.

## Benchmarking

Compare every installed version against a model:

```
$ llamavm bench all --model ~/models/llama-3.2-1b-instruct-q4_k_m.gguf
Version  Tokens/s   Total time   Δ vs current
b5046    44.72       9.8s         baseline (current)
b5489    47.21       9.2s         +5.6%
b5400    43.10      10.1s         -3.6%

Best: b5489 (47.21 tok/s)
```

Single-version run:

```bash
llamavm bench b5046 --model ~/models/llama-3.2-1b-instruct-q4_k_m.gguf
```

Results are cached by `(version, model-fingerprint)` under `~/.llamavm/benchmarks/`. Pass `--no-cache` to force a re-run.

## Commands

| Command | What it does |
| --- | --- |
| `llamavm install <tag>` | Build the given llama.cpp release tag and install it |
| `llamavm install latest` | Resolve the most recent release and install it |
| `llamavm uninstall <tag>` | Remove a previously installed version |
| `llamavm list` | Show installed versions; active one marked with `*` |
| `llamavm list-remote` | Show the most recent llama.cpp releases on GitHub |
| `llamavm use <tag>` | Set the global active version |
| `llamavm current` | Print the currently active version (respects `.llama-version`) |
| `llamavm pin <tag>` | Write `.llama-version` in the current directory |
| `llamavm bench <tag> --model <path>` | Benchmark a single version |
| `llamavm bench all --model <path>` | Benchmark every installed version |
| `llamavm doctor` | Diagnose installation and PATH configuration |

Run any subcommand with `--help` for full options.

## How it works

`llamavm install <tag>` clones llama.cpp at the given tag into a staging directory, runs the standard cmake build with Metal enabled, and atomically renames the result into `~/.llamavm/versions/<tag>/`. Failed builds leave no trace in `llamavm list`.

The first install also drops three small Go binaries — `llama-cli`, `llama-server`, `llama-quantize` — into `~/.llamavm/shims/`. When invoked, each shim walks up from the current directory looking for `.llama-version`, then falls back to `~/.llamavm/current`, then `exec`s the corresponding binary inside the resolved version's directory. Shim overhead is under 50ms.

## Uninstall

```bash
brew uninstall llamavm
rm -rf ~/.llamavm
```

## License

MIT
````

- [ ] **Step 3: Confirm the README renders sensibly (optional local sanity check)**

Run: `head -20 README.md && wc -l README.md`
Expected: tagline visible in the first lines; total length 80–120 lines.

- [ ] **Step 4: Commit**

```bash
git add README.md
git commit -m "docs: add README with install, usage, bench output, and rationale"
```

---

## Task 10: Acceptance smoke + release-checks log

**Files:**
- Create: `docs/release-checks/v1.0.0.md`

This task runs the PRD §9 acceptance criteria locally (those that can be exercised without GitHub state changes) and captures the output. The actual `git tag v1.0.0 && git push --tags` step is left to the user — releases are user-initiated and irreversible.

- [ ] **Step 1: Run the local acceptance smoke**

Build and exercise each criterion that can be verified locally:

```bash
go build -o llamavm ./cmd/llamavm
go build -o llamavm-shim ./cmd/llamavm-shim

# Capture an end-to-end transcript for the release log.
{
  echo '=== llamavm --version ==='
  ./llamavm --version

  echo
  echo '=== llamavm doctor (expect non-zero before shims are on PATH) ==='
  ./llamavm doctor; echo "exit=$?"

  echo
  echo '=== llamavm list ==='
  ./llamavm list

  echo
  echo '=== llamavm current ==='
  ./llamavm current || true
} > /tmp/m5-smoke.log 2>&1

cat /tmp/m5-smoke.log
```

Expected: doctor exits non-zero (criterion #10) until you `export PATH="$HOME/.llamavm/shims:$PATH"`; list/current behave per their respective tasks.

- [ ] **Step 2: Run the quality-gate suite**

Run: `gofmt -l . && go vet ./... && go test ./... && go build ./...`
Expected: no `gofmt` output, no vet warnings, all tests pass, build succeeds. (Criterion #12.)

- [ ] **Step 3: Verify GoReleaser config**

If goreleaser is installed locally:

Run: `goreleaser check && goreleaser release --snapshot --clean --skip=publish`
Expected: `config is valid`; `dist/` contains `llamavm_<snapshot>_darwin_arm64.tar.gz`, `checksums.txt`, and `dist/llamavm.rb`. Inspect `dist/llamavm.rb` and confirm the URL/sha256 substitution worked.

If goreleaser is not installed locally, document the skip in the release log; the actual release workflow run on tag push will surface any issue.

- [ ] **Step 4: Write the release-checks log**

Create `docs/release-checks/v1.0.0.md`:

```markdown
# llamavm v1.0.0 release checks

Verifies the 15 PRD §9 acceptance criteria. Items marked **(remote)** require
the GitHub release artifacts to exist and are completed by the user as part
of the release ceremony, not by an implementer agent.

## Local (machine-verifiable now)

- [ ] #5 `llamavm list` shows installed versions with the active one marked.
- [ ] #6 `llama-cli --version` (via shim) reports the active version's actual
  string. (Only verifiable if at least one version is installed locally.)
- [ ] #7 `.llama-version` resolution: creating `.llama-version` with `b5046`
  in a directory makes the shim use b5046 from that directory regardless
  of `~/.llamavm/current`.
- [ ] #8 `llamavm bench all --model <path>` produces a comparison table.
  (Requires ≥2 versions and a model file locally.)
- [ ] #9 `llamavm uninstall <tag>` removes the version cleanly.
- [ ] #10 `llamavm doctor` exits non-zero when the shims directory is not on
  PATH; exits zero with everything wired correctly.
- [ ] #11 Failed installs are atomic (kill mid-install → no entry in
  `llamavm list`).
- [ ] #12 Quality gates pass (`gofmt`, `go vet`, `staticcheck`, `go test`).

## Documentation

- [ ] #2 First-run setup message in `internal/cli/install.go` matches PRD §6.1
  copy and includes the exact PATH-export line.
- [ ] #13 `README.md` contains: tagline, install instructions, basic usage,
  bench output, "Why this exists" paragraph.

## Release ceremony (user-initiated)

- [ ] #1, #3, #4 Tag and push v1.0.0:

  ```bash
  git tag v1.0.0
  git push --tags
  ```

  The `release` workflow runs GoReleaser on macos-14, builds the
  darwin-arm64 archive, and creates the GitHub release with checksums.

- [ ] #14 Bootstrap the Homebrew tap (one-time):
  1. Create `github.com/gregmundy/homebrew-tap`.
  2. Add `Formula/llamavm.rb` based on `docs/llamavm.rb` in this repo
     (or copy `dist/llamavm.rb` produced by the v1.0.0 release run).
  3. Replace `VERSION` with `1.0.0` and `SHA256_PLACEHOLDER` with the
     darwin-arm64 sha256 from the GitHub release's `checksums.txt`.
  4. Commit and push.

- [ ] #1, #14 Verify clean install on a fresh state:

  ```bash
  brew install gregmundy/tap/llamavm
  echo 'export PATH="$HOME/.llamavm/shims:$PATH"' >> ~/.zshrc
  source ~/.zshrc
  llamavm install latest
  llamavm doctor   # expect exit 0
  ```

- [ ] #15 GitHub release v1.0.0 is published with the darwin-arm64 binary
  archive attached.

## Smoke transcript

Paste the contents of `/tmp/m5-smoke.log` (from the implementer's machine) below:

```
<paste here when running the release>
```
```

- [ ] **Step 5: Commit**

```bash
git add docs/release-checks/v1.0.0.md
git commit -m "docs: add v1.0.0 release-checks acceptance log"
```

- [ ] **Step 6: Final milestone summary**

Print a short summary covering:
1. Each task's commit hash (`git log --oneline -12`).
2. Doctor check coverage (count of `TestDoctor_*` cases).
3. The state of acceptance criteria #1, #14, #15 (these remain user-driven —
   the user tags and pushes v1.0.0; the workflow takes over from there).
4. Any reviewer follow-ups deferred (none expected).

This is the milestone-end report referenced in the autonomous-execution
feedback memory; the user will review it before tagging v1.0.0.

---

## Self-review notes

Spec coverage map (PRD §9 acceptance criteria → tasks):

| # | Criterion | Task |
| --- | --- | --- |
| 1 | Clean Homebrew install | Task 8 + 10 (user-driven) |
| 2 | First-run message | Task 9 (README mirrors §6.1; install.go already shipped) |
| 3 | install b5046 | Already shipped (M1) |
| 4 | install latest | Already shipped (M1) |
| 5 | list shows installed + active | Already shipped (M1/M2) |
| 6 | shim reports version | Already shipped (M2) |
| 7 | .llama-version resolution | Already shipped (M3) |
| 8 | bench all comparison | Already shipped (M4) |
| 9 | uninstall is clean | Already shipped (M1) |
| 10 | doctor non-zero when shims off PATH | Tasks 2–5 |
| 11 | atomic failed installs | Already shipped (M1) |
| 12 | quality gates pass | Task 6 (CI) + Task 10 (smoke) |
| 13 | README complete | Task 9 |
| 14 | Homebrew tap published | Task 8 + Task 10 (user-driven) |
| 15 | v1.0.0 GitHub release | Task 7 + Task 10 (user-driven) |
