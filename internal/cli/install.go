package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	gh "github.com/gregmundy/llamavm/internal/github"
	"github.com/gregmundy/llamavm/internal/version"
)

const llamaCppRepoURL = "https://github.com/ggml-org/llama.cpp.git"

var binaryNames = []string{"llama-cli", "llama-server", "llama-quantize"}

func newInstallCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "install <version>",
		Short: "Build and install a llama.cpp version from source",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInstall(cmd.Context(), deps, args[0])
		},
	}
}

func runInstall(ctx context.Context, deps *Deps, requested string) error {
	tag := requested
	if tag == "latest" {
		resolved, err := deps.GitHub.Latest(ctx)
		if err != nil {
			return fmt.Errorf("resolve latest: %w", err)
		}
		tag = resolved
	}

	if deps.Store.IsInstalled(tag) {
		fmt.Fprintf(deps.Stdout, "%s is already installed\n", tag)
		return nil
	}

	if err := deps.GitHub.TagExists(ctx, tag); err != nil {
		if errors.Is(err, gh.ErrTagNotFound) {
			return fmt.Errorf("version %s not found upstream", tag)
		}
		return fmt.Errorf("validate %s: %w", tag, err)
	}

	if !deps.Platform.IsAppleSilicon() {
		return fmt.Errorf("llamavm v1 requires Apple Silicon (darwin/arm64)")
	}

	staging := deps.Store.StagingDir(tag)
	source := filepath.Join(staging, "source")
	// Ensure no leftover from a previous failure.
	if err := deps.Store.RemoveStaging(tag); err != nil {
		return fmt.Errorf("clean staging: %w", err)
	}
	if err := os.MkdirAll(staging, 0o755); err != nil {
		return fmt.Errorf("create staging: %w", err)
	}

	var buildLog bytes.Buffer
	start := time.Now()
	fmt.Fprintf(deps.Stdout, "Installing %s...\n", tag)

	cleanup := func(phase string, runErr error) error {
		logPath, logErr := writeFailureLog(deps, tag, &buildLog)
		_ = deps.Store.RemoveStaging(tag)
		if logErr != nil {
			return fmt.Errorf("%s failed: %w (also failed to write log: %v)", phase, runErr, logErr)
		}
		return fmt.Errorf("%s failed: %w (build log: %s)", phase, runErr, logPath)
	}

	// git clone --depth 1 --branch <tag> <repo> <source>
	cloneArgs := []string{"clone", "--depth", "1", "--branch", tag, llamaCppRepoURL, source}
	if err := deps.Git.Run(ctx, "git", cloneArgs, "", &buildLog, &buildLog); err != nil {
		return cleanup("git clone", err)
	}

	if err := deps.Builder.Build(ctx, source, &buildLog); err != nil {
		return cleanup("build", err)
	}

	if err := symlinkBinaries(staging, source); err != nil {
		return cleanup("link binaries", err)
	}

	if err := deps.Store.PromoteStaging(tag); err != nil {
		return cleanup("promote staging", err)
	}

	// Set active if no active version is set.
	if _, err := deps.Store.Active(); errors.Is(err, version.ErrNoActiveVersion) {
		if err := deps.Store.SetActive(tag); err != nil {
			return fmt.Errorf("set active: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("read active: %w", err)
	}

	elapsed := time.Since(start).Round(time.Second)
	fmt.Fprintf(deps.Stdout, "Installed %s in %s\n", tag, elapsed)
	return nil
}

// symlinkBinaries creates staging/bin/<name> -> staging/source/build/bin/<name>
// for each binary in binaryNames.
func symlinkBinaries(staging, source string) error {
	binDir := filepath.Join(staging, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return fmt.Errorf("create bin dir: %w", err)
	}
	for _, name := range binaryNames {
		target := filepath.Join(source, "build", "bin", name)
		if _, err := os.Stat(target); err != nil {
			return fmt.Errorf("missing built binary %s: %w", name, err)
		}
		link := filepath.Join(binDir, name)
		if err := os.Symlink(target, link); err != nil {
			return fmt.Errorf("symlink %s: %w", name, err)
		}
	}
	return nil
}

// writeFailureLog drains the in-memory build log into <logsDir>/<tag>-<ts>.log.
func writeFailureLog(deps *Deps, tag string, body io.Reader) (string, error) {
	now := time.Now().UTC()
	if deps.Now != nil {
		now = deps.Now().UTC()
	}
	ts := now.Format("20060102T150405")
	logsDir := deps.Store.LogsDir()
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(logsDir, fmt.Sprintf("%s-%s.log", tag, ts))
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := io.Copy(f, body); err != nil {
		return path, err
	}
	return path, nil
}
