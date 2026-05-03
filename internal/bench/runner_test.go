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
// scripted body to stdout/stderr. install_name_tool calls (made by the
// lazy-migration path) are recorded but produce no output.
type fakeCmdRunner struct {
	calls    []recordedCall
	stdoutFn func(io.Writer)
	stderrFn func(io.Writer)
	err      error // returned for non-install_name_tool calls
}

type recordedCall struct {
	Name string
	Args []string
}

func (r *fakeCmdRunner) Run(_ context.Context, name string, args []string, _ string, stdout, stderr io.Writer) error {
	r.calls = append(r.calls, recordedCall{Name: name, Args: append([]string(nil), args...)})
	if name == "install_name_tool" {
		return nil // lazy-migration calls are tolerated regardless of outcome
	}
	if r.stdoutFn != nil {
		r.stdoutFn(stdout)
	}
	if r.stderrFn != nil {
		r.stderrFn(stderr)
	}
	return r.err
}

// benchCalls returns just the recorded calls that aren't install_name_tool.
// Tests use this to assert on the actual benchmark invocation while
// ignoring lazy-migration noise.
func (r *fakeCmdRunner) benchCalls() []recordedCall {
	out := []recordedCall{}
	for _, c := range r.calls {
		if c.Name != "install_name_tool" {
			out = append(out, c)
		}
	}
	return out
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

// touchLlamaBenchInstalled simulates a v1.1.5+ install: creates both the
// source-tree binary AND the bin/llama-bench symlink, mirroring what
// install.go does. Returns the symlink path the runner will resolve to.
func touchLlamaBenchInstalled(t *testing.T, versionsDir, tag string) string {
	t.Helper()
	versionDir := filepath.Join(versionsDir, tag)
	srcBin := filepath.Join(versionDir, "source", "build", "bin")
	if err := os.MkdirAll(srcBin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeExe(filepath.Join(srcBin, "llama-bench")); err != nil {
		t.Fatal(err)
	}
	binDir := filepath.Join(versionDir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(binDir, "llama-bench")
	if err := os.Symlink(filepath.Join("..", "source", "build", "bin", "llama-bench"), link); err != nil {
		t.Fatal(err)
	}
	return link
}

// touchLlamaBenchLegacy simulates a pre-v1.1.5 install: only the source-tree
// binary exists, no bin/llama-bench symlink. Used to exercise the runner's
// lazy-migration path.
func touchLlamaBenchLegacy(t *testing.T, versionsDir, tag string) {
	t.Helper()
	srcBin := filepath.Join(versionsDir, tag, "source", "build", "bin")
	if err := os.MkdirAll(srcBin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeExe(filepath.Join(srcBin, "llama-bench")); err != nil {
		t.Fatal(err)
	}
}

func TestRunner_HappyPath_PassesLlamaBenchArgs(t *testing.T) {
	model := writeModelFile(t, fillBytes(8192, 'm'))
	r := newRunnerWithCache(t, &fakeCmdRunner{
		stdoutFn: func(w io.Writer) { _, _ = w.Write([]byte(llamaBenchStdout)) },
	}, time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC))
	bin := touchLlamaBenchInstalled(t, r.VersionsDir, "b5046")

	res, err := r.Run(context.Background(), "b5046", model, true)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Cached {
		t.Fatal("first call should be a cache miss → not Cached")
	}
	// llamaBenchStdout's tg128 row is 35.24 t/s.
	if res.Version != "b5046" || res.TokensPerSec < 35.0 || res.TokensPerSec > 35.5 {
		t.Fatalf("Result = %+v, want Version=b5046 and TokensPerSec~35.24", res)
	}
	cmd := r.Cmd.(*fakeCmdRunner)
	bench := cmd.benchCalls()
	if len(bench) != 1 || bench[0].Name != bin {
		t.Fatalf("invoked %+v, want one call to %q", bench, bin)
	}
	wantArgs := []string{"-m", model, "-p", "256", "-n", "128", "-ngl", "99", "-r", "1"}
	if !equalStrings(bench[0].Args, wantArgs) {
		t.Fatalf("argv = %v\nwant      %v", bench[0].Args, wantArgs)
	}
}

func TestRunner_LegacyInstallTriggersLazyMigration(t *testing.T) {
	// Pre-v1.1.5 install: source binary exists but bin/llama-bench does not.
	// The runner must create the symlink AND run install_name_tool to fix
	// the rpath, then proceed with the bench.
	model := writeModelFile(t, fillBytes(8192, 'm'))
	r := newRunnerWithCache(t, &fakeCmdRunner{
		stdoutFn: func(w io.Writer) { _, _ = w.Write([]byte(llamaBenchStdout)) },
	}, time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC))
	touchLlamaBenchLegacy(t, r.VersionsDir, "b5046")

	res, err := r.Run(context.Background(), "b5046", model, true)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.TokensPerSec < 35.0 {
		t.Fatalf("TokensPerSec = %v, want fresh ~35.24", res.TokensPerSec)
	}
	// Verify the symlink was created with a relative target.
	link := filepath.Join(r.VersionsDir, "b5046", "bin", "llama-bench")
	target, err := os.Readlink(link)
	if err != nil {
		t.Fatalf("expected symlink at %s: %v", link, err)
	}
	if filepath.IsAbs(target) {
		t.Errorf("symlink target %q is absolute; want relative", target)
	}
	// Verify install_name_tool was invoked twice (delete + add).
	cmd := r.Cmd.(*fakeCmdRunner)
	intCalls := 0
	for _, c := range cmd.calls {
		if c.Name == "install_name_tool" {
			intCalls++
		}
	}
	if intCalls != 2 {
		t.Errorf("install_name_tool called %d times, want 2 (delete + add)", intCalls)
	}
}

func TestRunner_CacheHitSkipsExec(t *testing.T) {
	model := writeModelFile(t, fillBytes(8192, 'm'))
	r := newRunnerWithCache(t, &fakeCmdRunner{}, time.Now())
	touchLlamaBenchInstalled(t, r.VersionsDir, "b5046")

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
	if len(r.Cmd.(*fakeCmdRunner).benchCalls()) != 0 {
		t.Fatalf("expected 0 bench exec calls on cache hit; got %d",
			len(r.Cmd.(*fakeCmdRunner).benchCalls()))
	}
}

func TestRunner_NoCacheBypassesCache(t *testing.T) {
	model := writeModelFile(t, fillBytes(8192, 'm'))
	r := newRunnerWithCache(t, &fakeCmdRunner{
		stdoutFn: func(w io.Writer) { _, _ = w.Write([]byte(llamaBenchStdout)) },
	}, time.Now())
	touchLlamaBenchInstalled(t, r.VersionsDir, "b5046")

	fp, _ := Fingerprint(model)
	_ = r.Cache.Store(Result{Version: "b5046", ModelFingerprint: fp, TokensPerSec: 1.1, TotalTimeSeconds: 1.0, RanAt: time.Now()})

	res, err := r.Run(context.Background(), "b5046", model, false)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Cached {
		t.Fatal("--no-cache should not return cached")
	}
	if res.TokensPerSec < 35.0 {
		t.Fatalf("TokensPerSec = %v, want fresh ~35.24 (cached was 1.1)", res.TokensPerSec)
	}
}

func TestRunner_FreshResultIsCached(t *testing.T) {
	model := writeModelFile(t, fillBytes(8192, 'm'))
	r := newRunnerWithCache(t, &fakeCmdRunner{
		stdoutFn: func(w io.Writer) { _, _ = w.Write([]byte(llamaBenchStdout)) },
	}, time.Now())
	touchLlamaBenchInstalled(t, r.VersionsDir, "b5046")

	if _, err := r.Run(context.Background(), "b5046", model, true); err != nil {
		t.Fatalf("first Run: %v", err)
	}
	fp, _ := Fingerprint(model)
	cached, err := r.Cache.Lookup("b5046", fp)
	if err != nil {
		t.Fatalf("expected cache to be populated after Run: %v", err)
	}
	if cached.TokensPerSec < 35.0 {
		t.Fatalf("cached TokensPerSec = %v, want ~35.24", cached.TokensPerSec)
	}
}

func TestRunner_MissingModelIsError(t *testing.T) {
	r := newRunnerWithCache(t, &fakeCmdRunner{}, time.Now())
	touchLlamaBenchInstalled(t, r.VersionsDir, "b5046")
	_, err := r.Run(context.Background(), "b5046", "/no/such/model.gguf", true)
	if err == nil {
		t.Fatal("expected error for missing model")
	}
	if !errors.Is(err, ErrModelNotFound) {
		t.Fatalf("err = %v, want ErrModelNotFound", err)
	}
}

func TestRunner_VersionWithNoBinaryIsError(t *testing.T) {
	model := writeModelFile(t, fillBytes(8192, 'm'))
	r := newRunnerWithCache(t, &fakeCmdRunner{}, time.Now())
	// Note: not calling either touchLlamaBench helper — version dir has nothing.
	_, err := r.Run(context.Background(), "b5046", model, true)
	if err == nil {
		t.Fatal("expected error when llama-bench binary is missing")
	}
	if !errors.Is(err, ErrBinaryNotFound) {
		t.Fatalf("err = %v, want ErrBinaryNotFound", err)
	}
}

func TestRunner_ExecFailureBubblesUp(t *testing.T) {
	model := writeModelFile(t, fillBytes(8192, 'm'))
	r := newRunnerWithCache(t, &fakeCmdRunner{err: errors.New("exit status 1")}, time.Now())
	touchLlamaBenchInstalled(t, r.VersionsDir, "b5046")
	_, err := r.Run(context.Background(), "b5046", model, true)
	if err == nil {
		t.Fatal("expected exec error to surface")
	}
}

func TestRunner_UnparseableStdoutIsErrParse(t *testing.T) {
	model := writeModelFile(t, fillBytes(8192, 'm'))
	r := newRunnerWithCache(t, &fakeCmdRunner{
		stdoutFn: func(w io.Writer) { _, _ = w.Write([]byte("totally unrelated output\n")) },
	}, time.Now())
	touchLlamaBenchInstalled(t, r.VersionsDir, "b5046")
	_, err := r.Run(context.Background(), "b5046", model, true)
	if !errors.Is(err, ErrParse) {
		t.Fatalf("err = %v, want chained ErrParse", err)
	}
}

func TestRunner_NoCacheStillWritesCache(t *testing.T) {
	model := writeModelFile(t, fillBytes(8192, 'm'))
	r := newRunnerWithCache(t, &fakeCmdRunner{
		stdoutFn: func(w io.Writer) { _, _ = w.Write([]byte(llamaBenchStdout)) },
	}, time.Now())
	touchLlamaBenchInstalled(t, r.VersionsDir, "b5046")

	if _, err := r.Run(context.Background(), "b5046", model, false); err != nil {
		t.Fatalf("Run: %v", err)
	}
	fp, _ := Fingerprint(model)
	cached, err := r.Cache.Lookup("b5046", fp)
	if err != nil {
		t.Fatalf("--no-cache should still write the cache; Lookup: %v", err)
	}
	if cached.TokensPerSec < 35.0 {
		t.Fatalf("cached TokensPerSec = %v, want fresh ~35.24", cached.TokensPerSec)
	}
}

func TestRunner_CacheWriteFailureIsNonFatal(t *testing.T) {
	parent := t.TempDir()
	blockingFile := filepath.Join(parent, "block")
	if err := os.WriteFile(blockingFile, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	model := writeModelFile(t, fillBytes(8192, 'm'))
	r := &Runner{
		Cmd: &fakeCmdRunner{
			stdoutFn: func(w io.Writer) { _, _ = w.Write([]byte(llamaBenchStdout)) },
		},
		Cache:       &Cache{Dir: blockingFile},
		VersionsDir: t.TempDir(),
		Now:         func() time.Time { return time.Now() },
	}
	touchLlamaBenchInstalled(t, r.VersionsDir, "b5046")

	res, err := r.Run(context.Background(), "b5046", model, true)
	if err != nil {
		t.Fatalf("cache write failure should be non-fatal: %v", err)
	}
	if res.TokensPerSec < 35.0 {
		t.Fatalf("TokensPerSec = %v, want fresh ~35.24", res.TokensPerSec)
	}
}

func TestRunner_NilNowDefaultsToTimeNow(t *testing.T) {
	model := writeModelFile(t, fillBytes(8192, 'm'))
	r := &Runner{
		Cmd: &fakeCmdRunner{
			stdoutFn: func(w io.Writer) { _, _ = w.Write([]byte(llamaBenchStdout)) },
		},
		Cache:       &Cache{Dir: t.TempDir()},
		VersionsDir: t.TempDir(),
	}
	touchLlamaBenchInstalled(t, r.VersionsDir, "b5046")
	res, err := r.Run(context.Background(), "b5046", model, true)
	if err != nil {
		t.Fatalf("nil Now should default to time.Now: %v", err)
	}
	if res.RanAt.IsZero() {
		t.Fatal("RanAt should be set even with nil Now")
	}
}

func TestRunner_TotalTimeSecondsIsWallClock(t *testing.T) {
	// Wire a Now that advances by 7.5s on the second call; verify the Result's
	// TotalTimeSeconds reflects that.
	model := writeModelFile(t, fillBytes(8192, 'm'))
	start := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	calls := 0
	r := &Runner{
		Cmd: &fakeCmdRunner{
			stdoutFn: func(w io.Writer) { _, _ = w.Write([]byte(llamaBenchStdout)) },
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
				return start.Add(8 * time.Second)
			}
		},
	}
	touchLlamaBenchInstalled(t, r.VersionsDir, "b5046")

	res, err := r.Run(context.Background(), "b5046", model, true)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.TotalTimeSeconds < 7.49 || res.TotalTimeSeconds > 7.51 {
		t.Fatalf("TotalTimeSeconds = %v, want ~7.5 (wall-clock from mocked Now)", res.TotalTimeSeconds)
	}
}

// writeExe / equalStrings — small package-private helpers for the runner
// tests. Defined here rather than in cache_test.go to keep that file
// focused on cache concerns.

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
