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

// BenchmarkPrompt is the fixed prompt PRD §3.10.1 hardcodes for v1. Kept as
// an exported var so it's discoverable; not configurable in v1.
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
	ErrBinaryNotFound = fmt.Errorf("llama-cli binary not found for version: %w", fs.ErrNotExist)
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
// llama-cli runs with the PRD §3.10.1 fixed argv, stderr is parsed, and the
// fresh Result is written to the cache.
func (r *Runner) Run(ctx context.Context, tag, modelPath string, useCache bool) (Result, error) {
	if _, err := os.Stat(modelPath); err != nil {
		if os.IsNotExist(err) {
			return Result{}, fmt.Errorf("%s: %w", modelPath, ErrModelNotFound)
		}
		return Result{}, fmt.Errorf("stat model: %w", err)
	}

	// Mirror the model branch above: only treat NotExist as ErrBinaryNotFound
	// (which the cli maps to a "run llamavm install" remediation). Other stat
	// errors — permission denied, EIO, etc. — must propagate so the user sees
	// the actual problem rather than a misleading "not installed" message.
	bin := filepath.Join(r.VersionsDir, tag, "bin", "llama-cli")
	if _, err := os.Stat(bin); err != nil {
		if os.IsNotExist(err) {
			return Result{}, fmt.Errorf("%s (%s): %w", tag, bin, ErrBinaryNotFound)
		}
		return Result{}, fmt.Errorf("stat llama-cli: %w", err)
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
		"-p", BenchmarkPrompt,
		"-n", "256",
		"--no-display-prompt",
		"-ngl", "99",
		// -st (single-turn) tells llama-cli to exit after one model response
		// instead of auto-entering conversation mode for chat-templated models.
		// Without it, chat-tuned models (Gemma-it, Llama-Instruct, etc.) loop
		// forever printing the conv-mode `>` prompt while waiting for stdin
		// that never arrives, producing GBs of garbage and never returning.
		"-st",
	}
	now := r.Now
	if now == nil {
		now = time.Now
	}
	// Wall-clock measurement: includes model load + setup overhead, which is
	// what the user actually waits through. Replaces the parser's old
	// `total time =` extraction (the b9010+ output format dropped that line).
	var stdout, stderr bytes.Buffer
	start := now()
	if err := r.Cmd.Run(ctx, bin, args, "", &stdout, &stderr); err != nil {
		return Result{}, fmt.Errorf("run llama-cli: %w", err)
	}
	elapsed := now().Sub(start)

	// New format (b9010+) lives on stdout; legacy llama_perf_* lines live on
	// stderr. Concat so Parse can match either without the runner needing to
	// know which version it's talking to.
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
