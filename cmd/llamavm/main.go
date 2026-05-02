package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gregmundy/llamavm/internal/builder"
	"github.com/gregmundy/llamavm/internal/cli"
	gh "github.com/gregmundy/llamavm/internal/github"
	"github.com/gregmundy/llamavm/internal/version"
)

var llamavmVersion = "dev"

func main() {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "llamavm: cannot resolve home directory:", err)
		os.Exit(1)
	}

	platform := builder.DefaultPlatform
	store := version.New(home)
	runner := builder.ExecRunner{}

	deps := &cli.Deps{
		Store:    store,
		GitHub:   gh.New(),
		Builder:  &builder.Builder{Runner: runner, Platform: platform},
		Git:      runner,
		Platform: platform,
		Stdout:   os.Stdout,
		Stderr:   os.Stderr,
		Now:      time.Now,
	}

	root := cli.NewRoot(deps, llamavmVersion)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := root.ExecuteContext(ctx); err != nil {
		// Cobra prints the error itself; we translate to PRD §6.4 exit codes.
		if errors.Is(err, cli.ErrUserError) {
			os.Exit(2)
		}
		os.Exit(1)
	}
}
