package cli

import (
	"errors"
	"strings"
	"testing"

	"github.com/gregmundy/llamavm/internal/bench"
)

func TestBench_RequiresModelFlag(t *testing.T) {
	deps := &Deps{
		Store:       &fakeStore{installed: []string{"b5046"}},
		Benchmarker: &fakeBenchmarker{},
	}
	if _, _, err := runRoot(t, deps, "bench", "b5046"); err == nil {
		t.Fatal("expected error when --model is missing")
	}
}

func TestBench_RequiresArg(t *testing.T) {
	deps := &Deps{Store: &fakeStore{}, Benchmarker: &fakeBenchmarker{}}
	if _, _, err := runRoot(t, deps, "bench", "--model", "/x/y.gguf"); err == nil {
		t.Fatal("expected error when version arg missing")
	}
}

func TestBench_VersionNotInstalledIsUserError(t *testing.T) {
	deps := &Deps{
		Store:       &fakeStore{installed: []string{"b5489"}},
		Benchmarker: &fakeBenchmarker{},
	}
	_, _, err := runRoot(t, deps, "bench", "b5046", "--model", "/x/y.gguf")
	if err == nil {
		t.Fatal("expected error when version not installed")
	}
	if !errors.Is(err, ErrUserError) {
		t.Fatalf("err = %v, want chained ErrUserError", err)
	}
}

func TestBench_HappyPathPrintsTokensPerSec(t *testing.T) {
	bm := &fakeBenchmarker{
		results: []bench.Result{{
			Version: "b5046", TokensPerSec: 41.7, TotalTimeSeconds: 16.9,
		}},
	}
	deps := &Deps{
		Store:       &fakeStore{installed: []string{"b5046"}},
		Benchmarker: bm,
	}
	out, _, err := runRoot(t, deps, "bench", "b5046", "--model", "/x/y.gguf")
	if err != nil {
		t.Fatalf("bench: %v", err)
	}
	if !strings.Contains(out, "b5046") {
		t.Fatalf("stdout = %q, want it to mention b5046", out)
	}
	if !strings.Contains(out, "41.7") {
		t.Fatalf("stdout = %q, want it to mention tokens/sec 41.7", out)
	}
	if !strings.Contains(out, "16.9") {
		t.Fatalf("stdout = %q, want it to mention total time 16.9", out)
	}
	if len(bm.calls) != 1 {
		t.Fatalf("expected 1 benchmarker call, got %d", len(bm.calls))
	}
	if bm.calls[0].useCache != true {
		t.Fatalf("default useCache should be true; got %v", bm.calls[0].useCache)
	}
	if bm.calls[0].model != "/x/y.gguf" {
		t.Fatalf("model = %q, want /x/y.gguf", bm.calls[0].model)
	}
}

func TestBench_NoCacheFlagPropagates(t *testing.T) {
	bm := &fakeBenchmarker{
		results: []bench.Result{{Version: "b5046", TokensPerSec: 1, TotalTimeSeconds: 1}},
	}
	deps := &Deps{
		Store:       &fakeStore{installed: []string{"b5046"}},
		Benchmarker: bm,
	}
	if _, _, err := runRoot(t, deps, "bench", "b5046", "--model", "/x/y.gguf", "--no-cache"); err != nil {
		t.Fatalf("bench: %v", err)
	}
	if bm.calls[0].useCache {
		t.Fatal("--no-cache should make useCache=false")
	}
}

func TestBench_CachedResultIsLabeled(t *testing.T) {
	bm := &fakeBenchmarker{
		results: []bench.Result{{
			Version: "b5046", TokensPerSec: 41.7, TotalTimeSeconds: 16.9, Cached: true,
		}},
	}
	deps := &Deps{
		Store:       &fakeStore{installed: []string{"b5046"}},
		Benchmarker: bm,
	}
	out, _, err := runRoot(t, deps, "bench", "b5046", "--model", "/x/y.gguf")
	if err != nil {
		t.Fatalf("bench: %v", err)
	}
	if !strings.Contains(out, "(cached)") {
		t.Fatalf("stdout = %q, want it to contain literal '(cached)'", out)
	}
}

func TestBench_BinaryNotFoundIsUserError(t *testing.T) {
	bm := &fakeBenchmarker{errs: []error{bench.ErrBinaryNotFound}}
	deps := &Deps{
		Store:       &fakeStore{installed: []string{"b5046"}},
		Benchmarker: bm,
	}
	_, _, err := runRoot(t, deps, "bench", "b5046", "--model", "/x/y.gguf")
	if err == nil {
		t.Fatal("expected error when binary missing")
	}
	if !errors.Is(err, ErrUserError) {
		t.Fatalf("err = %v, want chained ErrUserError", err)
	}
}

func TestBench_ModelNotFoundIsUserError(t *testing.T) {
	bm := &fakeBenchmarker{errs: []error{bench.ErrModelNotFound}}
	deps := &Deps{
		Store:       &fakeStore{installed: []string{"b5046"}},
		Benchmarker: bm,
	}
	_, _, err := runRoot(t, deps, "bench", "b5046", "--model", "/no/such")
	if err == nil {
		t.Fatal("expected error when model missing")
	}
	if !errors.Is(err, ErrUserError) {
		t.Fatalf("err = %v, want chained ErrUserError", err)
	}
}

func TestBench_RunnerErrorPropagates(t *testing.T) {
	bm := &fakeBenchmarker{errs: []error{errors.New("boom")}}
	deps := &Deps{
		Store:       &fakeStore{installed: []string{"b5046"}},
		Benchmarker: bm,
	}
	if _, _, err := runRoot(t, deps, "bench", "b5046", "--model", "/x/y.gguf"); err == nil {
		t.Fatal("expected error to propagate")
	}
}

func TestBenchAll_NoVersionsInstalled(t *testing.T) {
	deps := &Deps{Store: &fakeStore{}, Benchmarker: &fakeBenchmarker{}}
	_, _, err := runRoot(t, deps, "bench", "all", "--model", "/x/y.gguf")
	if err == nil {
		t.Fatal("expected error when no versions installed")
	}
	if !errors.Is(err, ErrUserError) {
		t.Fatalf("err = %v, want chained ErrUserError", err)
	}
}

func TestBenchAll_RunsEachVersionAndPrintsTable(t *testing.T) {
	bm := &fakeBenchmarker{
		results: []bench.Result{
			{Version: "b5046", TokensPerSec: 41.7, TotalTimeSeconds: 16.9},
			{Version: "b5400", TokensPerSec: 39.1, TotalTimeSeconds: 18.0},
			{Version: "b5489", TokensPerSec: 38.2, TotalTimeSeconds: 18.4},
		},
	}
	deps := &Deps{
		Store: &fakeStore{
			installed: []string{"b5046", "b5400", "b5489"},
			active:    "b5489",
			hasActive: true,
		},
		Benchmarker: bm,
	}
	out, _, err := runRoot(t, deps, "bench", "all", "--model", "/x/y.gguf")
	if err != nil {
		t.Fatalf("bench all: %v", err)
	}
	// Header
	if !strings.Contains(out, "Version") || !strings.Contains(out, "Tokens/sec") || !strings.Contains(out, "Status") {
		t.Fatalf("missing header columns; out=\n%s", out)
	}
	// All versions present
	for _, v := range []string{"b5046", "b5400", "b5489"} {
		if !strings.Contains(out, v) {
			t.Fatalf("version %s missing from table; out=\n%s", v, out)
		}
	}
	// Active version marked "current"
	if !strings.Contains(out, "current") {
		t.Fatalf("active version should be marked 'current'; out=\n%s", out)
	}
	// Faster version shows "+X.X% vs current"
	if !strings.Contains(out, "vs current") {
		t.Fatalf("non-active versions should show delta vs current; out=\n%s", out)
	}
	// Best line
	if !strings.Contains(out, "Best:") {
		t.Fatalf("output should include a Best: line; out=\n%s", out)
	}
	if !strings.Contains(out, "b5046") {
		t.Fatal("Best line should mention the fastest version (b5046)")
	}
	if len(bm.calls) != 3 {
		t.Fatalf("benchmarker calls = %d, want 3", len(bm.calls))
	}
}

func TestBenchAll_ContinuesPastFailures(t *testing.T) {
	bm := &fakeBenchmarker{
		results: []bench.Result{
			{Version: "b5046", TokensPerSec: 41.7, TotalTimeSeconds: 16.9},
			{}, // index 1 will use the err below
			{Version: "b5489", TokensPerSec: 38.2, TotalTimeSeconds: 18.4},
		},
		errs: []error{nil, errors.New("kaboom"), nil},
	}
	deps := &Deps{
		Store: &fakeStore{
			installed: []string{"b5046", "b5400", "b5489"},
			active:    "b5489",
			hasActive: true,
		},
		Benchmarker: bm,
	}
	out, _, err := runRoot(t, deps, "bench", "all", "--model", "/x/y.gguf")
	if err != nil {
		t.Fatalf("bench all: %v (should continue past per-version failures)", err)
	}
	if !strings.Contains(out, "b5400") {
		t.Fatalf("failed version should still appear in table; out=\n%s", out)
	}
	if !strings.Contains(strings.ToLower(out), "failed") {
		t.Fatalf("failed row should be labeled; out=\n%s", out)
	}
	// Best must consider only successful runs.
	if !strings.Contains(out, "b5046") || !strings.Contains(out, "Best:") {
		t.Fatalf("Best line should still appear with successful winner; out=\n%s", out)
	}
}

func TestBenchAll_NoActiveVersion_NoCurrentMarker(t *testing.T) {
	bm := &fakeBenchmarker{
		results: []bench.Result{
			{Version: "b5046", TokensPerSec: 41.7, TotalTimeSeconds: 16.9},
			{Version: "b5489", TokensPerSec: 38.2, TotalTimeSeconds: 18.4},
		},
	}
	deps := &Deps{
		Store: &fakeStore{
			installed: []string{"b5046", "b5489"}, // hasActive=false
		},
		Benchmarker: bm,
	}
	out, _, err := runRoot(t, deps, "bench", "all", "--model", "/x/y.gguf")
	if err != nil {
		t.Fatalf("bench all: %v", err)
	}
	if strings.Contains(out, "current") {
		t.Fatalf("with no active version, table should NOT contain 'current'; out=\n%s", out)
	}
	if strings.Contains(out, "vs current") {
		t.Fatalf("with no active version, table should NOT contain 'vs current'; out=\n%s", out)
	}
}

func TestBenchAll_WithinNoiseShowsApproxCurrent(t *testing.T) {
	// b9009 vs active b9010: 36.1 vs 36.0 = +0.28% — under the 0.5%
	// noise threshold. Status should read "≈ current", not "+0.3% vs current".
	bm := &fakeBenchmarker{
		results: []bench.Result{
			{Version: "b9009", TokensPerSec: 36.1, TotalTimeSeconds: 5.1},
			{Version: "b9010", TokensPerSec: 36.0, TotalTimeSeconds: 5.1},
		},
	}
	deps := &Deps{
		Store: &fakeStore{
			installed: []string{"b9009", "b9010"},
			active:    "b9010",
			hasActive: true,
		},
		Benchmarker: bm,
	}
	out, _, err := runRoot(t, deps, "bench", "all", "--model", "/x/y.gguf")
	if err != nil {
		t.Fatalf("bench all: %v", err)
	}
	if !strings.Contains(out, "≈ current") {
		t.Fatalf("expected '≈ current' in within-noise comparison; out=\n%s", out)
	}
	// And the misleading +0.3% line should NOT appear.
	if strings.Contains(out, "+0.3%") || strings.Contains(out, "0.3% vs current") {
		t.Fatalf("within-noise rows should not show a small percentage; out=\n%s", out)
	}
}

func TestBenchAll_BeyondNoiseShowsPercentage(t *testing.T) {
	// 5% delta is well above noise — keep the existing percentage display.
	bm := &fakeBenchmarker{
		results: []bench.Result{
			{Version: "b9009", TokensPerSec: 38.0, TotalTimeSeconds: 5.0},
			{Version: "b9010", TokensPerSec: 36.0, TotalTimeSeconds: 5.1},
		},
	}
	deps := &Deps{
		Store: &fakeStore{
			installed: []string{"b9009", "b9010"},
			active:    "b9010",
			hasActive: true,
		},
		Benchmarker: bm,
	}
	out, _, err := runRoot(t, deps, "bench", "all", "--model", "/x/y.gguf")
	if err != nil {
		t.Fatalf("bench all: %v", err)
	}
	if !strings.Contains(out, "+5.6% vs current") {
		t.Fatalf("expected '+5.6%% vs current'; out=\n%s", out)
	}
}

func TestBenchAll_BestWithinNoiseSaysCurrent(t *testing.T) {
	// Best is b9009 (36.1) but within 0.5% of active b9010 (36.0) — Best line
	// should not crown a non-winner.
	bm := &fakeBenchmarker{
		results: []bench.Result{
			{Version: "b9009", TokensPerSec: 36.1, TotalTimeSeconds: 5.1},
			{Version: "b9010", TokensPerSec: 36.0, TotalTimeSeconds: 5.1},
		},
	}
	deps := &Deps{
		Store: &fakeStore{
			installed: []string{"b9009", "b9010"},
			active:    "b9010",
			hasActive: true,
		},
		Benchmarker: bm,
	}
	out, _, err := runRoot(t, deps, "bench", "all", "--model", "/x/y.gguf")
	if err != nil {
		t.Fatalf("bench all: %v", err)
	}
	if !strings.Contains(out, "Best: current (within noise)") {
		t.Fatalf("expected 'Best: current (within noise)'; out=\n%s", out)
	}
	// And the older "Best: b9009 (...)" line should NOT appear.
	if strings.Contains(out, "Best: b9009") {
		t.Fatalf("Best line should not crown b9009 when it's within noise of current; out=\n%s", out)
	}
}

func TestBenchAll_BestBeyondNoiseShowsTag(t *testing.T) {
	// b9009 at 38.0 vs current b9010 at 36.0 → +5.6%, well above noise.
	// Best line keeps the existing form.
	bm := &fakeBenchmarker{
		results: []bench.Result{
			{Version: "b9009", TokensPerSec: 38.0, TotalTimeSeconds: 5.0},
			{Version: "b9010", TokensPerSec: 36.0, TotalTimeSeconds: 5.1},
		},
	}
	deps := &Deps{
		Store: &fakeStore{
			installed: []string{"b9009", "b9010"},
			active:    "b9010",
			hasActive: true,
		},
		Benchmarker: bm,
	}
	out, _, err := runRoot(t, deps, "bench", "all", "--model", "/x/y.gguf")
	if err != nil {
		t.Fatalf("bench all: %v", err)
	}
	if !strings.Contains(out, "Best: b9009 (38.0 t/s)") {
		t.Fatalf("expected 'Best: b9009 (38.0 t/s)'; out=\n%s", out)
	}
}
