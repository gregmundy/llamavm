package bench

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// BenchmarkPrompt is the fixed prompt PRD §3.10.1 hardcodes for v1.
// Retained as an exported var for backwards compatibility — llama-bench
// uses synthetic token counts (-p / -n) rather than a real prompt, so this
// constant is no longer passed to the binary.
var BenchmarkPrompt = "Write a detailed 200-word summary of the French Revolution. Include key dates, figures, and outcomes."

// CommandRunner abstracts os/exec for testability. Same shape as
// internal/builder.CommandRunner so production wiring can pass the existing
// builder.ExecRunner directly.
type CommandRunner interface {
	Run(ctx context.Context, name string, args []string, dir string, stdout, stderr io.Writer) error
}

// Sentinel errors callers can match against to map to PRD §6.4 exit codes.
// Both wrap fs.ErrNotExist so callers using errors.Is(err, fs.ErrNotExist) —
// a more general "the thing is missing" check — also match.
var (
	ErrModelNotFound  = fmt.Errorf("model file not found: %w", fs.ErrNotExist)
	ErrBinaryNotFound = fmt.Errorf("llama-bench binary not found for version: %w", fs.ErrNotExist)
)

// Runner orchestrates a single benchmark: cache lookup, exec, parse, cache write.
type Runner struct {
	Cmd         CommandRunner
	Cache       *Cache
	VersionsDir string // <home>/.llamavm/versions
	Now         func() time.Time
}

// Run benchmarks tag against modelPath. If useCache is true and a cached
// Result exists for (tag, fingerprint), it's returned without exec. Otherwise
// llama-bench runs with -p 256 -n 128 -ngl 99 -r 1, the markdown table on
// stdout is parsed for the tg128 row's t/s, and the fresh Result is cached.
//
// llama-bench is preferred over llama-cli because it (a) doesn't auto-enable
// conversation mode for chat-tuned models, (b) doesn't render an interactive
// UI to /dev/tty when invoked from a TTY-attached shell, and (c) emits
// structured output that's stable across llama.cpp releases.
func (r *Runner) Run(ctx context.Context, tag, modelPath string, useCache bool) (Result, error) {
	if _, err := os.Stat(modelPath); err != nil {
		if os.IsNotExist(err) {
			return Result{}, fmt.Errorf("%s: %w", modelPath, ErrModelNotFound)
		}
		return Result{}, fmt.Errorf("stat model: %w", err)
	}

	versionDir := filepath.Join(r.VersionsDir, tag)
	bin, err := r.ensureBenchBinary(ctx, versionDir)
	if err != nil {
		return Result{}, fmt.Errorf("%s: %w", tag, err)
	}

	fp, err := Fingerprint(modelPath)
	if err != nil {
		return Result{}, fmt.Errorf("fingerprint: %w", err)
	}

	if useCache {
		if cached, err := r.Cache.Lookup(tag, fp); err == nil {
			return cached, nil
		}
	}

	args := []string{
		"-m", modelPath,
		"-p", "256", // prompt-processing token count
		"-n", "128", // generation token count
		"-ngl", "99",
		"-r", "1", // single repetition; we average across versions, not within
	}
	now := r.Now
	if now == nil {
		now = time.Now
	}
	var stdout, stderr bytes.Buffer
	start := now()
	if err := r.Cmd.Run(ctx, bin, args, "", &stdout, &stderr); err != nil {
		return Result{}, fmt.Errorf("run llama-bench: %w", err)
	}
	elapsed := now().Sub(start)

	stats, err := Parse(stdout.String() + stderr.String())
	if err != nil {
		return Result{}, fmt.Errorf("%s: %w", tag, err)
	}

	res := Result{
		Version:          tag,
		ModelFingerprint: fp,
		TokensPerSec:     stats.TokensPerSec,
		TotalTimeSeconds: elapsed.Seconds(),
		RanAt:            now().UTC(),
	}
	if err := r.Cache.Store(res); err != nil {
		// Cache write failure is non-fatal — return the result anyway.
		// The user still gets the bench output; next run will simply re-run.
		fmt.Fprintf(os.Stderr, "llamavm: warning: cache write failed: %v\n", err)
	}
	return res, nil
}

// ensureBenchBinary returns the symlink path to a working llama-bench for
// versionDir. For versions installed under llamavm < 1.1.5, llama-bench
// wasn't symlinked into <tag>/bin and its embedded LC_RPATH still points
// at the staging dir (broken after install promotion). This function
// lazily creates the symlink AND rewrites the rpath to @loader_path so
// the bench can run without requiring a full reinstall.
func (r *Runner) ensureBenchBinary(ctx context.Context, versionDir string) (string, error) {
	binDir := filepath.Join(versionDir, "bin")
	symlink := filepath.Join(binDir, "llama-bench")
	if _, err := os.Stat(symlink); err == nil {
		// v1.1.5+ install: symlink is present and the binary was relocated
		// by the install pipeline. Trust it.
		return symlink, nil
	}

	// Legacy install: verify the source binary exists, then create the
	// symlink and relocate.
	sourceBuildBin := filepath.Join(versionDir, "source", "build", "bin")
	actual := filepath.Join(sourceBuildBin, "llama-bench")
	if _, err := os.Stat(actual); err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("(%s): %w", actual, ErrBinaryNotFound)
		}
		return "", fmt.Errorf("stat llama-bench: %w", err)
	}
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return "", fmt.Errorf("ensure bin dir: %w", err)
	}
	relTarget := filepath.Join("..", "source", "build", "bin", "llama-bench")
	if err := os.Symlink(relTarget, symlink); err != nil {
		return "", fmt.Errorf("create llama-bench symlink: %w", err)
	}
	// Rewrite rpath. Tolerate "rpath not present" on delete and "rpath
	// already exists" on add — either failure means the binary is already
	// in the desired state (e.g. a partial migration on a previous run).
	_ = r.Cmd.Run(ctx, "install_name_tool",
		[]string{"-delete_rpath", sourceBuildBin, actual}, "", io.Discard, io.Discard)
	_ = r.Cmd.Run(ctx, "install_name_tool",
		[]string{"-add_rpath", "@loader_path", actual}, "", io.Discard, io.Discard)
	return symlink, nil
}
