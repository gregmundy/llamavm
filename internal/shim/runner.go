// Package shim implements the llama-* shim binary entry point and the
// installer that drops shim copies into ~/.llamavm/shims/.
package shim

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// ExitNotFound is the conventional exit code for "command not found"
// (PRD §6.4 and §3.12.2 require 127 for shim resolution failures).
const ExitNotFound = 127

// Options is the dependency surface for Run. All OS-touching behavior
// is injected so unit tests can run without exec, syscalls, or hard-coded
// home directories.
type Options struct {
	Argv        []string
	Resolver    func() (string, error)
	VersionsDir string
	Stderr      io.Writer
	Env         []string
	ExecFn      func(path string, argv, envv []string) error
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
