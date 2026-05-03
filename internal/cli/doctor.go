package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
		// Doctor already prints a per-check transcript and a FAIL summary to
		// stdout; cobra's default `Error: ...` line on RunE failure would just
		// duplicate that on stderr, so silence it. Exit code still flows via
		// ErrUserError → main.go → os.Exit(2).
		SilenceErrors: true,
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

// doctorCheck pairs a human-readable label and remediation hint with the
// predicate that determines pass/fail. run returns true on pass.
type doctorCheck struct {
	label       string
	remediation string
	run         func(ctx context.Context, deps *Deps) bool
}

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
		label:       "every ~/.llamavm/shims/llama-* shim resolves to a binary in the active version",
		remediation: "run 'llamavm install <version>' (or reinstall to pick up shims for new tools)",
		run: func(_ context.Context, deps *Deps) bool {
			shimsDir := deps.Store.ShimsDir()
			entries, err := os.ReadDir(shimsDir)
			if err != nil {
				return false
			}
			// Need at least one llama-* shim to consider this check passing —
			// catches the "no install has run yet" state.
			active, _ := deps.Store.Active()
			seen := 0
			for _, e := range entries {
				name := e.Name()
				if !strings.HasPrefix(name, llamaBinaryPrefix) {
					continue
				}
				seen++
				if active == "" {
					// Can't verify the shim resolves without an active version.
					// Subsequent checks (#4, #5) cover that case directly.
					continue
				}
				binPath := filepath.Join(deps.Store.VersionDir(active), "bin", name)
				if _, err := os.Stat(binPath); err != nil {
					return false
				}
			}
			return seen > 0
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
