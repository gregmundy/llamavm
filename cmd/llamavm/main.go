package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/gregmundy/llamavm/internal/bench"
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
	if resolved, err := filepath.EvalSymlinks(self); err == nil {
		self = resolved
	}
	return filepath.Join(filepath.Dir(self), "llamavm-shim"), nil
}

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

func main() {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "llamavm: cannot resolve home directory:", err)
		os.Exit(1)
	}

	platform := builder.DefaultPlatform
	store := version.New(home)
	runner := builder.ExecRunner{}
	resolver := version.NewResolver(store, home)
	installer := &shim.Installer{Source: defaultShimSource}
	benchmarker := &bench.Runner{
		Cmd:         runner,
		Cache:       &bench.Cache{Dir: store.BenchmarksDir()},
		VersionsDir: store.VersionsDir(),
		Now:         time.Now,
	}

	deps := &cli.Deps{
		Store:           store,
		GitHub:          gh.New(),
		Builder:         &builder.Builder{Runner: runner, Platform: platform},
		Git:             runner,
		Platform:        platform,
		Resolver:        resolver,
		ShimInstaller:   installer,
		Benchmarker:     benchmarker,
		Stdout:          os.Stdout,
		Stderr:          os.Stderr,
		Now:             time.Now,
		Getwd:           os.Getwd,
		LookPath:        exec.LookPath,
		Getenv:          os.Getenv,
		XcodeSelectPath: defaultXcodeSelectPath(runner),
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
