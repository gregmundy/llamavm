package bench

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// fakeCmdRunner records the arguments it was called with and writes a
// scripted body to stdout/stderr.
type fakeCmdRunner struct {
	gotName  string
	gotArgs  []string
	gotDir   string
	stdoutFn func(io.Writer)
	stderrFn func(io.Writer)
	err      error
	calls    int
}

func (r *fakeCmdRunner) Run(_ context.Context, name string, args []string, dir string, stdout, stderr io.Writer) error {
	r.gotName = name
	r.gotArgs = append([]string(nil), args...)
	r.gotDir = dir
	r.calls++
	if r.stdoutFn != nil {
		r.stdoutFn(stdout)
	}
	if r.stderrFn != nil {
		r.stderrFn(stderr)
	}
	return r.err
}

func newRunnerWithCache(t *testing.T, runner CommandRunner, fixedNow time.Time) *Runner {
	t.Helper()
	return &Runner{
		Cmd:         runner,
		Cache:       &Cache{Dir: t.TempDir()},
		VersionsDir: t.TempDir(),
		Now:         func() time.Time { return fixedNow },
	}
}

// touchLlamaCLI creates a llama-cli binary stub at the path the runner
// expects: <VersionsDir>/<tag>/bin/llama-cli.
func touchLlamaCLI(t *testing.T, versionsDir, tag string) string {
	t.Helper()
	dir := filepath.Join(versionsDir, tag, "bin")
	if err := makeDir(dir); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "llama-cli")
	if err := writeExe(bin); err != nil {
		t.Fatal(err)
	}
	return bin
}

func TestRunner_HappyPath_PassesPRDArgs(t *testing.T) {
	model := writeModelFile(t, fillBytes(8192, 'm'))
	r := newRunnerWithCache(t, &fakeCmdRunner{
		stderrFn: func(w io.Writer) { _, _ = w.Write([]byte(modernStderr)) },
	}, time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC))
	cli := touchLlamaCLI(t, r.VersionsDir, "b5046")

	res, err := r.Run(context.Background(), "b5046", model, true)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Cached {
		t.Fatal("first call should be a cache miss → not Cached")
	}
	if res.Version != "b5046" || res.TokensPerSec < 44.7 {
		t.Fatalf("Result = %+v, want Version=b5046 and TokensPerSec~44.72", res)
	}
	cmd := r.Cmd.(*fakeCmdRunner)
	if cmd.gotName != cli {
		t.Fatalf("invoked %q, want %q", cmd.gotName, cli)
	}
	wantArgs := []string{"-m", model, "-p", BenchmarkPrompt, "-n", "256", "--no-display-prompt", "-ngl", "99", "-st"}
	if !equalStrings(cmd.gotArgs, wantArgs) {
		t.Fatalf("argv = %v\nwant      %v", cmd.gotArgs, wantArgs)
	}
}

func TestRunner_CacheHitSkipsExec(t *testing.T) {
	model := writeModelFile(t, fillBytes(8192, 'm'))
	r := newRunnerWithCache(t, &fakeCmdRunner{}, time.Now())
	touchLlamaCLI(t, r.VersionsDir, "b5046")

	fp, err := Fingerprint(model)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Cache.Store(Result{
		Version: "b5046", ModelFingerprint: fp,
		TokensPerSec: 99.9, TotalTimeSeconds: 1.0,
		RanAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	res, err := r.Run(context.Background(), "b5046", model, true)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.Cached {
		t.Fatal("expected Cached=true on hit")
	}
	if res.TokensPerSec != 99.9 {
		t.Fatalf("TokensPerSec = %v, want 99.9 (cached value)", res.TokensPerSec)
	}
	if r.Cmd.(*fakeCmdRunner).calls != 0 {
		t.Fatalf("expected 0 exec calls on cache hit; got %d", r.Cmd.(*fakeCmdRunner).calls)
	}
}

func TestRunner_NoCacheBypassesCache(t *testing.T) {
	model := writeModelFile(t, fillBytes(8192, 'm'))
	r := newRunnerWithCache(t, &fakeCmdRunner{
		stderrFn: func(w io.Writer) { _, _ = w.Write([]byte(modernStderr)) },
	}, time.Now())
	touchLlamaCLI(t, r.VersionsDir, "b5046")

	fp, _ := Fingerprint(model)
	_ = r.Cache.Store(Result{Version: "b5046", ModelFingerprint: fp, TokensPerSec: 1.1, TotalTimeSeconds: 1.0, RanAt: time.Now()})

	res, err := r.Run(context.Background(), "b5046", model, false /*useCache*/)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Cached {
		t.Fatal("--no-cache should not return cached")
	}
	if res.TokensPerSec < 44.0 {
		t.Fatalf("TokensPerSec = %v, want fresh ~44.72 (cached was 1.1)", res.TokensPerSec)
	}
	if r.Cmd.(*fakeCmdRunner).calls != 1 {
		t.Fatalf("expected 1 exec call when --no-cache; got %d", r.Cmd.(*fakeCmdRunner).calls)
	}
}

func TestRunner_FreshResultIsCached(t *testing.T) {
	model := writeModelFile(t, fillBytes(8192, 'm'))
	r := newRunnerWithCache(t, &fakeCmdRunner{
		stderrFn: func(w io.Writer) { _, _ = w.Write([]byte(modernStderr)) },
	}, time.Now())
	touchLlamaCLI(t, r.VersionsDir, "b5046")

	if _, err := r.Run(context.Background(), "b5046", model, true); err != nil {
		t.Fatalf("first Run: %v", err)
	}
	fp, _ := Fingerprint(model)
	cached, err := r.Cache.Lookup("b5046", fp)
	if err != nil {
		t.Fatalf("expected cache to be populated after Run: %v", err)
	}
	if cached.TokensPerSec < 44.0 {
		t.Fatalf("cached TokensPerSec = %v, want ~44.72", cached.TokensPerSec)
	}
}

func TestRunner_MissingModelIsError(t *testing.T) {
	r := newRunnerWithCache(t, &fakeCmdRunner{}, time.Now())
	touchLlamaCLI(t, r.VersionsDir, "b5046")
	_, err := r.Run(context.Background(), "b5046", "/no/such/model.gguf", true)
	if err == nil {
		t.Fatal("expected error for missing model")
	}
	if !errors.Is(err, ErrModelNotFound) {
		t.Fatalf("err = %v, want ErrModelNotFound", err)
	}
	if r.Cmd.(*fakeCmdRunner).calls != 0 {
		t.Fatal("should not exec when model is missing")
	}
}

func TestRunner_VersionWithNoBinaryIsError(t *testing.T) {
	model := writeModelFile(t, fillBytes(8192, 'm'))
	r := newRunnerWithCache(t, &fakeCmdRunner{}, time.Now())
	// Note: not calling touchLlamaCLI — version dir has no binary.
	_, err := r.Run(context.Background(), "b5046", model, true)
	if err == nil {
		t.Fatal("expected error when llama-cli binary is missing")
	}
	if !errors.Is(err, ErrBinaryNotFound) {
		t.Fatalf("err = %v, want ErrBinaryNotFound", err)
	}
}

func TestRunner_ExecFailureBubblesUp(t *testing.T) {
	model := writeModelFile(t, fillBytes(8192, 'm'))
	r := newRunnerWithCache(t, &fakeCmdRunner{err: errors.New("exit status 1")}, time.Now())
	touchLlamaCLI(t, r.VersionsDir, "b5046")
	_, err := r.Run(context.Background(), "b5046", model, true)
	if err == nil {
		t.Fatal("expected exec error to surface")
	}
}

func TestRunner_UnparseableStderrIsErrParse(t *testing.T) {
	model := writeModelFile(t, fillBytes(8192, 'm'))
	r := newRunnerWithCache(t, &fakeCmdRunner{
		stderrFn: func(w io.Writer) { _, _ = w.Write([]byte("totally unrelated output\n")) },
	}, time.Now())
	touchLlamaCLI(t, r.VersionsDir, "b5046")
	_, err := r.Run(context.Background(), "b5046", model, true)
	if !errors.Is(err, ErrParse) {
		t.Fatalf("err = %v, want chained ErrParse", err)
	}
}

func TestRunner_NoCacheStillWritesCache(t *testing.T) {
	// Regression guard: --no-cache must still write the fresh result back.
	model := writeModelFile(t, fillBytes(8192, 'm'))
	r := newRunnerWithCache(t, &fakeCmdRunner{
		stderrFn: func(w io.Writer) { _, _ = w.Write([]byte(modernStderr)) },
	}, time.Now())
	touchLlamaCLI(t, r.VersionsDir, "b5046")

	if _, err := r.Run(context.Background(), "b5046", model, false /*useCache*/); err != nil {
		t.Fatalf("Run: %v", err)
	}
	fp, _ := Fingerprint(model)
	cached, err := r.Cache.Lookup("b5046", fp)
	if err != nil {
		t.Fatalf("--no-cache should still write the cache; Lookup: %v", err)
	}
	if cached.TokensPerSec < 44.0 {
		t.Fatalf("cached TokensPerSec = %v, want fresh ~44.72", cached.TokensPerSec)
	}
}

func TestRunner_CacheWriteFailureIsNonFatal(t *testing.T) {
	// Point Cache.Dir at a path that already exists as a regular file so
	// Cache.Store's MkdirAll fails. The runner should still return the
	// fresh result rather than erroring out.
	parent := t.TempDir()
	blockingFile := filepath.Join(parent, "block")
	if err := os.WriteFile(blockingFile, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	model := writeModelFile(t, fillBytes(8192, 'm'))
	r := &Runner{
		Cmd: &fakeCmdRunner{
			stderrFn: func(w io.Writer) { _, _ = w.Write([]byte(modernStderr)) },
		},
		Cache:       &Cache{Dir: blockingFile}, // MkdirAll on this path fails
		VersionsDir: t.TempDir(),
		Now:         func() time.Time { return time.Now() },
	}
	touchLlamaCLI(t, r.VersionsDir, "b5046")

	res, err := r.Run(context.Background(), "b5046", model, true)
	if err != nil {
		t.Fatalf("cache write failure should be non-fatal: %v", err)
	}
	if res.TokensPerSec < 44.0 {
		t.Fatalf("TokensPerSec = %v, want fresh ~44.72", res.TokensPerSec)
	}
}

func TestRunner_NilNowDefaultsToTimeNow(t *testing.T) {
	model := writeModelFile(t, fillBytes(8192, 'm'))
	r := &Runner{
		Cmd: &fakeCmdRunner{
			stderrFn: func(w io.Writer) { _, _ = w.Write([]byte(modernStderr)) },
		},
		Cache:       &Cache{Dir: t.TempDir()},
		VersionsDir: t.TempDir(),
		// Now intentionally nil to exercise the default.
	}
	touchLlamaCLI(t, r.VersionsDir, "b5046")
	res, err := r.Run(context.Background(), "b5046", model, true)
	if err != nil {
		t.Fatalf("nil Now should default to time.Now: %v", err)
	}
	if res.RanAt.IsZero() {
		t.Fatal("RanAt should be set even with nil Now")
	}
}

func TestRunner_BinaryStatErrorIsNotMisreportedAsNotFound(t *testing.T) {
	// Put a *file* (not a dir) at the "<tag>/bin" path so os.Stat on the
	// joined "<tag>/bin/llama-cli" fails with something other than NotExist
	// (ENOTDIR). Runner must propagate the real error rather than translate
	// it to ErrBinaryNotFound.
	model := writeModelFile(t, fillBytes(8192, 'm'))
	r := newRunnerWithCache(t, &fakeCmdRunner{}, time.Now())
	tagDir := filepath.Join(r.VersionsDir, "b5046")
	if err := os.MkdirAll(tagDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Create "bin" as a file rather than a directory.
	if err := os.WriteFile(filepath.Join(tagDir, "bin"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := r.Run(context.Background(), "b5046", model, true)
	if err == nil {
		t.Fatal("expected error when binary path traversal is blocked")
	}
	if errors.Is(err, ErrBinaryNotFound) {
		t.Fatalf("err = %v, want a non-NotFound error (real cause is ENOTDIR)", err)
	}
}

func TestRunner_NewFormatOnStdoutIsParsed(t *testing.T) {
	// b9010+ writes the perf summary as a single line on STDOUT (not stderr).
	// Verify the runner concats both streams and the parser finds it.
	model := writeModelFile(t, fillBytes(8192, 'm'))
	r := newRunnerWithCache(t, &fakeCmdRunner{
		stdoutFn: func(w io.Writer) { _, _ = w.Write([]byte(newFormatStdout)) },
		// stderr left empty — only metal init noise in real builds, no
		// llama_perf_* lines anymore.
	}, time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC))
	touchLlamaCLI(t, r.VersionsDir, "b9010")

	res, err := r.Run(context.Background(), "b9010", model, true)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// newFormatStdout has "Generation: 33.6 t/s".
	if res.TokensPerSec < 33.5 || res.TokensPerSec > 33.7 {
		t.Fatalf("TokensPerSec = %v, want ~33.6 (parsed from new stdout format)", res.TokensPerSec)
	}
}

func TestRunner_TotalTimeSecondsIsWallClock(t *testing.T) {
	// TotalTimeSeconds must come from the runner's wall-clock measurement
	// (now() before exec, now() after), not from a parsed `total time =` line.
	// Wire a Now that advances by 7.5s on the second call and verify the
	// Result reflects that, even though the legacy fixture stderr says
	// "total time = 9756.14 ms" (parser doesn't read total time anymore).
	model := writeModelFile(t, fillBytes(8192, 'm'))
	start := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	calls := 0
	r := &Runner{
		Cmd: &fakeCmdRunner{
			stderrFn: func(w io.Writer) { _, _ = w.Write([]byte(modernStderr)) },
		},
		Cache:       &Cache{Dir: t.TempDir()},
		VersionsDir: t.TempDir(),
		Now: func() time.Time {
			calls++
			switch calls {
			case 1:
				return start
			case 2:
				return start.Add(7500 * time.Millisecond)
			default:
				return start.Add(8 * time.Second) // RanAt
			}
		},
	}
	touchLlamaCLI(t, r.VersionsDir, "b5046")

	res, err := r.Run(context.Background(), "b5046", model, true)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.TotalTimeSeconds < 7.49 || res.TotalTimeSeconds > 7.51 {
		t.Fatalf("TotalTimeSeconds = %v, want ~7.5 (wall-clock from mocked Now)", res.TotalTimeSeconds)
	}
}

// makeDir / writeExe / equalStrings — small package-private helpers for the
// runner tests. Defined here rather than in cache_test.go to keep that file
// focused on cache concerns.

func makeDir(p string) error {
	return os.MkdirAll(p, 0o755)
}

func writeExe(p string) error {
	return os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755)
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
