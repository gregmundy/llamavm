package bench

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
)

// ErrParse signals that the supplied stderr did not contain the lines we
// need to extract benchmark statistics. Callers in production wrap this with
// the version tag for easier diagnosis.
var ErrParse = errors.New("parse benchmark output")

// Stats is the parsed output of a single llama-cli run.
type Stats struct {
	TokensPerSec     float64
	TotalTimeSeconds float64
}

// Modern (b4000+): "llama_perf_context_print:        eval time =    7423.47 ms /   332 runs   (   22.36 ms per token,    44.72 tokens per second)"
// Legacy:          "llama_print_timings:             eval time =    4178.32 ms /    31 runs   (   134.78 ms per token)"
//
// We capture both ms-per-token and (optional) tokens-per-second. The trailing
// `[^)]*?` between ms-per-token and the tokens-per-second pair tolerates
// future llama.cpp builds that interleave additional metrics inside the parens
// (e.g. GFLOP/s) — keeping the inline tokens-per-second authoritative rather
// than silently falling back to a computed-from-ms value when the format
// drifts. `[^)]` cannot cross either the closing paren or a newline.
var evalLineRe = regexp.MustCompile(
	`(?m)^llama_(?:perf_context_print|print_timings):\s*eval time\s*=\s*[0-9.]+\s*ms\s*/\s*\d+\s*runs?\s*\(\s*([0-9.]+)\s*ms per token(?:[^)]*?,\s*([0-9.]+)\s*tokens per second)?`,
)

// Total time line: "...total time = 9756.14 ms" or "...total time = 9756.14 ms / 355 tokens"
var totalLineRe = regexp.MustCompile(
	`(?m)^llama_(?:perf_context_print|print_timings):\s*total time\s*=\s*([0-9.]+)\s*ms`,
)

// Parse extracts Stats from llama-cli stderr. Returns ErrParse if either the
// eval-time line or the total-time line is missing.
func Parse(stderr string) (Stats, error) {
	eval := evalLineRe.FindStringSubmatch(stderr)
	if eval == nil {
		return Stats{}, fmt.Errorf("eval-time line not found: %w", ErrParse)
	}
	msPerTok, err := strconv.ParseFloat(eval[1], 64)
	if err != nil {
		return Stats{}, fmt.Errorf("ms-per-token value %q: %w", eval[1], ErrParse)
	}
	var tokPerSec float64
	// Optional unmatched group is "" in Go's regexp, never out-of-bounds.
	if eval[2] != "" {
		v, err := strconv.ParseFloat(eval[2], 64)
		if err != nil {
			return Stats{}, fmt.Errorf("tokens-per-second value %q: %w", eval[2], ErrParse)
		}
		tokPerSec = v
	} else {
		if msPerTok == 0 {
			return Stats{}, fmt.Errorf("ms-per-token is zero (cannot compute tokens-per-second): %w", ErrParse)
		}
		tokPerSec = 1000.0 / msPerTok
	}

	total := totalLineRe.FindStringSubmatch(stderr)
	if total == nil {
		return Stats{}, fmt.Errorf("total-time line not found: %w", ErrParse)
	}
	totalMs, err := strconv.ParseFloat(total[1], 64)
	if err != nil {
		return Stats{}, fmt.Errorf("total-time value %q: %w", total[1], ErrParse)
	}

	return Stats{
		TokensPerSec:     tokPerSec,
		TotalTimeSeconds: totalMs / 1000.0,
	}, nil
}
