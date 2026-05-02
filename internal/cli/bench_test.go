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
	if !strings.Contains(strings.ToLower(out), "cached") {
		t.Fatalf("stdout = %q, want it to mark cached results", out)
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
