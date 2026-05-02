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
