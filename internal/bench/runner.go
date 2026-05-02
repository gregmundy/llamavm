package bench

import (
	stdbytes "bytes"
	"context"
	"errors"
	"fmt"
	"io"
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
var (
	ErrModelNotFound  = errors.New("model file not found")
	ErrBinaryNotFound = errors.New("llama-cli binary not found for version")
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

	bin := filepath.Join(r.VersionsDir, tag, "bin", "llama-cli")
	if _, err := os.Stat(bin); err != nil {
		return Result{}, fmt.Errorf("%s (%s): %w", tag, bin, ErrBinaryNotFound)
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
	}
	var stderr stdbytes.Buffer
	if err := r.Cmd.Run(ctx, bin, args, "", io.Discard, &stderr); err != nil {
		return Result{}, fmt.Errorf("run llama-cli: %w", err)
	}

	stats, err := Parse(stderr.String())
	if err != nil {
		return Result{}, fmt.Errorf("%s: %w", tag, err)
	}

	res := Result{
		Version:          tag,
		ModelFingerprint: fp,
		TokensPerSec:     stats.TokensPerSec,
		TotalTimeSeconds: stats.TotalTimeSeconds,
		RanAt:            r.Now().UTC(),
	}
	if err := r.Cache.Store(res); err != nil {
		// Cache write failure is non-fatal — return the result anyway.
		// The user still gets the bench output; next run will simply re-run.
		fmt.Fprintf(os.Stderr, "llamavm: warning: cache write failed: %v\n", err)
	}
	return res, nil
}
