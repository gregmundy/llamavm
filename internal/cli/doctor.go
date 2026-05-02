package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

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
	return []doctorCheck{
		checkRootDir(),
		checkShimFiles(),
		checkShimsOnPATH(),
		checkAtLeastOneVersion(),
		checkActiveVersionResolves(),
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
