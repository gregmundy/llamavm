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
