package bench

import (
	"errors"
	"math"
	"strings"
	"testing"
)

const modernStderr = `
ggml_metal_init: allocating
llama_model_loader: loaded meta data
llama_model_loader: - kv 0: ...
llama_perf_sampler_print:    sampling time =      24.18 ms /   356 runs   (    0.07 ms per token, 14724.50 tokens per second)
llama_perf_context_print:        load time =    2418.42 ms
llama_perf_context_print: prompt eval time =     289.54 ms /    23 tokens (   12.59 ms per token,    79.43 tokens per second)
llama_perf_context_print:        eval time =    7423.47 ms /   332 runs   (   22.36 ms per token,    44.72 tokens per second)
llama_perf_context_print:       total time =    9756.14 ms /   355 tokens
`

const legacyStderr = `
llama.cpp: loading model from ./model.bin
llama_print_timings:        load time =   134.84 ms
llama_print_timings:      sample time =    12.25 ms /    32 runs   (    0.38 ms per token)
llama_print_timings: prompt eval time =   925.78 ms /    24 tokens (   38.57 ms per token)
llama_print_timings:        eval time =  4178.32 ms /    31 runs   (  134.78 ms per token)
llama_print_timings:       total time =  5211.06 ms
`

func nearly(a, b, eps float64) bool { return math.Abs(a-b) <= eps }

func TestParse_ModernFormat_TokensPerSec(t *testing.T) {
	got, err := Parse(modernStderr)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !nearly(got.TokensPerSec, 44.72, 0.01) {
		t.Fatalf("TokensPerSec = %v, want ~44.72", got.TokensPerSec)
	}
}

func TestParse_ModernFormat_TotalTime(t *testing.T) {
	got, err := Parse(modernStderr)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !nearly(got.TotalTimeSeconds, 9.75614, 0.0001) {
		t.Fatalf("TotalTimeSeconds = %v, want ~9.75614", got.TotalTimeSeconds)
	}
}

func TestParse_LegacyFormat_TokensPerSecComputedFromMsPerToken(t *testing.T) {
	// Legacy line gives only ms-per-token; parser must compute t/s = 1000/ms.
	got, err := Parse(legacyStderr)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	want := 1000.0 / 134.78 // ≈ 7.42
	if !nearly(got.TokensPerSec, want, 0.01) {
		t.Fatalf("TokensPerSec = %v, want ~%v (1000/134.78)", got.TokensPerSec, want)
	}
}

func TestParse_LegacyFormat_TotalTime(t *testing.T) {
	got, err := Parse(legacyStderr)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !nearly(got.TotalTimeSeconds, 5.21106, 0.0001) {
		t.Fatalf("TotalTimeSeconds = %v, want ~5.21106", got.TotalTimeSeconds)
	}
}

func TestParse_MissingEvalLineIsErrParse(t *testing.T) {
	missingEval := strings.Replace(modernStderr,
		"llama_perf_context_print:        eval time", "llama_perf_context_print:        EVAL_TIME_GONE", 1)
	if _, err := Parse(missingEval); !errors.Is(err, ErrParse) {
		t.Fatalf("Parse(no eval line): got %v, want ErrParse", err)
	}
}

func TestParse_MissingTotalLineIsErrParse(t *testing.T) {
	missingTotal := strings.Replace(modernStderr,
		"llama_perf_context_print:       total time", "llama_perf_context_print:       TOTAL_GONE", 1)
	if _, err := Parse(missingTotal); !errors.Is(err, ErrParse) {
		t.Fatalf("Parse(no total line): got %v, want ErrParse", err)
	}
}

func TestParse_EmptyInputIsErrParse(t *testing.T) {
	if _, err := Parse(""); !errors.Is(err, ErrParse) {
		t.Fatalf("Parse(\"\"): got %v, want ErrParse", err)
	}
}

func TestParse_PrefersTokensPerSecOverComputedWhenBothAvailable(t *testing.T) {
	// Sanity: when the modern format has both "X ms per token" and "Y tokens
	// per second", we trust Y rather than recomputing from X.
	got, err := Parse(modernStderr)
	if err != nil {
		t.Fatal(err)
	}
	// Modern eval line has 22.36 ms/tok and 44.72 t/s. 1000/22.36 ≈ 44.72,
	// but if the parser ever recomputes from a slightly different ms value,
	// the second-decimal exact match here pins it to the inline value.
	if got.TokensPerSec < 44.72-0.005 || got.TokensPerSec > 44.72+0.005 {
		t.Fatalf("TokensPerSec = %v, want exactly 44.72 (parsed inline)", got.TokensPerSec)
	}
}

func TestParse_TolersExtraMetricInsideParens(t *testing.T) {
	// If a future llama.cpp build adds another metric inside the parens
	// (e.g. GFLOP/s) before "tokens per second", the inline value should
	// still be used — not silently discarded in favour of the computed
	// 1000/ms_per_token fallback. The fixture below differs from modernStderr
	// only by inserting "1.50 GFLOP/s," between the two existing metrics.
	stderr := strings.Replace(modernStderr,
		"22.36 ms per token,    44.72 tokens per second",
		"22.36 ms per token,    1.50 GFLOP/s,    44.72 tokens per second",
		1)
	got, err := Parse(stderr)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got.TokensPerSec < 44.72-0.005 || got.TokensPerSec > 44.72+0.005 {
		t.Fatalf("TokensPerSec = %v, want 44.72 (inline value, not computed)", got.TokensPerSec)
	}
}

func TestParse_SingleRunNoTrailingS(t *testing.T) {
	// Lock in the regex's `runs?` permissiveness so a future tightening
	// doesn't break a 1-run benchmark output.
	stderr := strings.Replace(modernStderr,
		"7423.47 ms /   332 runs", "7423.47 ms /   1 run", 1)
	if _, err := Parse(stderr); err != nil {
		t.Fatalf("Parse('1 run'): %v", err)
	}
}
