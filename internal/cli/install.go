package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/spf13/cobra"

	gh "github.com/gregmundy/llamavm/internal/github"
	"github.com/gregmundy/llamavm/internal/version"
)

const llamaCppRepoURL = "https://github.com/ggml-org/llama.cpp.git"

var binaryNames = []string{"llama-cli", "llama-server", "llama-quantize"}
var validTagRe = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

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

	if !validTagRe.MatchString(tag) {
		return fmt.Errorf("invalid version format %q: %w", tag, ErrUserError)
	}

	if deps.Store.IsInstalled(tag) {
		fmt.Fprintf(deps.Stdout, "%s is already installed\n", tag)
		return nil
	}

	if err := deps.GitHub.TagExists(ctx, tag); err != nil {
		if errors.Is(err, gh.ErrTagNotFound) {
			return fmt.Errorf("version %s not found upstream: %w", tag, ErrUserError)
		}
		return fmt.Errorf("validate %s: %w", tag, err)
	}

	if !deps.Platform.IsAppleSilicon() {
		return fmt.Errorf("llamavm v1 requires Apple Silicon (darwin/arm64): %w", ErrUserError)
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

	binDir := filepath.Join(source, "build", "bin")
	if err := relocateRpaths(ctx, deps.Git, binDir, &buildLog); err != nil {
		return cleanup("relocate rpaths", err)
	}

	if err := symlinkBinaries(staging, source); err != nil {
		return cleanup("link binaries", err)
	}

	if err := deps.Store.PromoteStaging(tag); err != nil {
		return cleanup("promote staging", err)
	}

	if err := deps.ShimInstaller.EnsureInstalled(deps.Store.ShimsDir()); err != nil {
		return fmt.Errorf("install shims: %w", err)
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

// relocateRpaths rewrites every Mach-O LC_RPATH that points at binDir
// (the absolute build-output dir) to @loader_path, so binaries and dylibs
// find each other relative to their own location after the staging→final
// dir rename. CMAKE_BUILD_RPATH_USE_ORIGIN=ON is set in builder.go but
// llama.cpp's CMakeLists pins LC_RPATH to the build dir explicitly,
// overriding the cmake hint — hence this post-build pass via macOS's
// install_name_tool (already required as part of Xcode CLT).
//
// Touches all *.dylib files plus the three llama-* binaries. Other tools
// produced by the build (e.g. export-graph-ops) are left alone since they
// are not exposed via shims.
func relocateRpaths(ctx context.Context, runner CommandRunner, binDir string, logWriter io.Writer) error {
	entries, err := os.ReadDir(binDir)
	if err != nil {
		return fmt.Errorf("read build/bin: %w", err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() {
			continue
		}
		// Skip symlinks: cmake produces a chain like
		//   libggml-base.dylib → libggml-base.0.dylib → libggml-base.0.10.2.dylib
		// where the first two are symlinks to the third. install_name_tool
		// dereferences them, so processing each name would call add_rpath
		// three times on the same underlying file and the second call fails
		// with "rpath already exists". Only touch the regular file.
		if entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		if !shouldRelocate(name) {
			continue
		}
		path := filepath.Join(binDir, name)
		// Delete the absolute build-dir LC_RPATH then add @loader_path.
		// install_name_tool's exit codes don't distinguish "no such rpath"
		// from a real error, so on delete we tolerate failure (the file may
		// legitimately have no matching LC_RPATH, e.g. a dylib that didn't
		// link other dylibs) and let add proceed.
		_ = runner.Run(ctx, "install_name_tool",
			[]string{"-delete_rpath", binDir, path}, "", logWriter, logWriter)
		if err := runner.Run(ctx, "install_name_tool",
			[]string{"-add_rpath", "@loader_path", path}, "", logWriter, logWriter); err != nil {
			return fmt.Errorf("install_name_tool -add_rpath %s: %w", name, err)
		}
	}
	return nil
}

// shouldRelocate returns true for files we want to make position-independent:
// any dylib, plus the three named binaries the shims dispatch to.
func shouldRelocate(name string) bool {
	if filepath.Ext(name) == ".dylib" {
		return true
	}
	for _, bin := range binaryNames {
		if name == bin {
			return true
		}
	}
	return false
}

// symlinkBinaries creates staging/bin/<name> -> ../source/build/bin/<name>
// for each binary in binaryNames. Targets are RELATIVE so the symlinks
// survive the staging→final dir rename in PromoteStaging — an absolute
// target would still spell `.staging-<tag>` after the parent's rename and
// dangle.
func symlinkBinaries(staging, source string) error {
	binDir := filepath.Join(staging, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return fmt.Errorf("create bin dir: %w", err)
	}
	for _, name := range binaryNames {
		absTarget := filepath.Join(source, "build", "bin", name)
		if _, err := os.Stat(absTarget); err != nil {
			return fmt.Errorf("missing built binary %s: %w", name, err)
		}
		link := filepath.Join(binDir, name)
		relTarget, err := filepath.Rel(binDir, absTarget)
		if err != nil {
			return fmt.Errorf("compute relative target for %s: %w", name, err)
		}
		if err := os.Symlink(relTarget, link); err != nil {
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
