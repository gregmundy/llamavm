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
// scripted body to stderr.
type fakeCmdRunner struct {
	gotName  string
	gotArgs  []string
	gotDir   string
	stderrFn func(io.Writer)
	err      error
	calls    int
}

func (r *fakeCmdRunner) Run(_ context.Context, name string, args []string, dir string, _, stderr io.Writer) error {
	r.gotName = name
	r.gotArgs = append([]string(nil), args...)
	r.gotDir = dir
	r.calls++
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
	model := writeModelFile(t, bytes(8192, 'm'))
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
	wantArgs := []string{"-m", model, "-p", BenchmarkPrompt, "-n", "256", "--no-display-prompt", "-ngl", "99"}
	if !equalStrings(cmd.gotArgs, wantArgs) {
		t.Fatalf("argv = %v\nwant      %v", cmd.gotArgs, wantArgs)
	}
}

func TestRunner_CacheHitSkipsExec(t *testing.T) {
	model := writeModelFile(t, bytes(8192, 'm'))
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
	model := writeModelFile(t, bytes(8192, 'm'))
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
	model := writeModelFile(t, bytes(8192, 'm'))
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
	model := writeModelFile(t, bytes(8192, 'm'))
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
	model := writeModelFile(t, bytes(8192, 'm'))
	r := newRunnerWithCache(t, &fakeCmdRunner{err: errors.New("exit status 1")}, time.Now())
	touchLlamaCLI(t, r.VersionsDir, "b5046")
	_, err := r.Run(context.Background(), "b5046", model, true)
	if err == nil {
		t.Fatal("expected exec error to surface")
	}
}

func TestRunner_UnparseableStderrIsErrParse(t *testing.T) {
	model := writeModelFile(t, bytes(8192, 'm'))
	r := newRunnerWithCache(t, &fakeCmdRunner{
		stderrFn: func(w io.Writer) { _, _ = w.Write([]byte("totally unrelated output\n")) },
	}, time.Now())
	touchLlamaCLI(t, r.VersionsDir, "b5046")
	_, err := r.Run(context.Background(), "b5046", model, true)
	if !errors.Is(err, ErrParse) {
		t.Fatalf("err = %v, want chained ErrParse", err)
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
