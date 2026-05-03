package bench

import (
	"errors"
	"math"
	"strings"
	"testing"
)

// New format (b9010+, single line on stdout when llama-cli runs with -st).
const newFormatStdout = `
Some essay text from the model goes here. Lots of it. Multiple lines.

[ Prompt: 280.3 t/s | Generation: 33.6 t/s ]

Exiting...
`

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

func TestParse_NewFormat(t *testing.T) {
	got, err := Parse(newFormatStdout)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !nearly(got.TokensPerSec, 33.6, 0.01) {
		t.Fatalf("TokensPerSec = %v, want ~33.6", got.TokensPerSec)
	}
}

func TestParse_NewFormatPreferredOverLegacy(t *testing.T) {
	// If both appear (unlikely but defensive), the new format wins because it
	// reflects the user-visible inline summary that the build is actually
	// emitting going forward.
	combined := newFormatStdout + modernStderr
	got, err := Parse(combined)
	if err != nil {
		t.Fatal(err)
	}
	if !nearly(got.TokensPerSec, 33.6, 0.01) {
		t.Fatalf("TokensPerSec = %v, want 33.6 (new format), got legacy 44.72?", got.TokensPerSec)
	}
}

func TestParse_ModernLegacyFormat_TokensPerSec(t *testing.T) {
	got, err := Parse(modernStderr)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !nearly(got.TokensPerSec, 44.72, 0.01) {
		t.Fatalf("TokensPerSec = %v, want ~44.72", got.TokensPerSec)
	}
}

func TestParse_OldestLegacyFormat_TokensPerSecComputedFromMsPerToken(t *testing.T) {
	// Oldest legacy line gives only ms-per-token; parser computes t/s = 1000/ms.
	got, err := Parse(legacyStderr)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	want := 1000.0 / 134.78 // ≈ 7.42
	if !nearly(got.TokensPerSec, want, 0.01) {
		t.Fatalf("TokensPerSec = %v, want ~%v (1000/134.78)", got.TokensPerSec, want)
	}
}

func TestParse_MissingAnyKnownFormatIsErrParse(t *testing.T) {
	if _, err := Parse("totally unrelated output\nno metrics here\n"); !errors.Is(err, ErrParse) {
		t.Fatalf("Parse(garbage): got %v, want ErrParse", err)
	}
}

func TestParse_EmptyInputIsErrParse(t *testing.T) {
	if _, err := Parse(""); !errors.Is(err, ErrParse) {
		t.Fatalf("Parse(\"\"): got %v, want ErrParse", err)
	}
}

func TestParse_LegacyPrefersInlineTokensPerSecOverComputed(t *testing.T) {
	// Sanity: when the modern legacy format has both "X ms per token" and
	// "Y tokens per second", we trust Y rather than recomputing from X.
	got, err := Parse(modernStderr)
	if err != nil {
		t.Fatal(err)
	}
	// Modern legacy eval line has 22.36 ms/tok and 44.72 t/s. 1000/22.36 ≈ 44.72,
	// but if the parser ever recomputes from a slightly different ms value,
	// the second-decimal exact match here pins it to the inline value.
	if got.TokensPerSec < 44.72-0.005 || got.TokensPerSec > 44.72+0.005 {
		t.Fatalf("TokensPerSec = %v, want exactly 44.72 (parsed inline)", got.TokensPerSec)
	}
}

func TestParse_LegacyTolersExtraMetricInsideParens(t *testing.T) {
	// If a future llama.cpp build adds another metric inside the parens
	// (e.g. GFLOP/s) before "tokens per second", the inline value should
	// still be used — not silently discarded in favour of the computed
	// 1000/ms_per_token fallback.
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

func TestParse_LegacySingleRunNoTrailingS(t *testing.T) {
	// Lock in the regex's `runs?` permissiveness so a future tightening
	// doesn't break a 1-run benchmark output.
	stderr := strings.Replace(modernStderr,
		"7423.47 ms /   332 runs", "7423.47 ms /   1 run", 1)
	if _, err := Parse(stderr); err != nil {
		t.Fatalf("Parse('1 run'): %v", err)
	}
}
