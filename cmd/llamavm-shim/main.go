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
