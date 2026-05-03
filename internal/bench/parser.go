package bench

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
)

// ErrParse signals that the supplied output did not contain a recognisable
// tokens/sec figure. Callers in production wrap this with the version tag
// for easier diagnosis.
var ErrParse = errors.New("parse benchmark output")

// Stats is the parsed throughput info from a single llama-cli run. Wall-clock
// total time is measured by the Runner separately (the new b9010+ output
// format doesn't carry a total-time figure, and the wall-clock number is more
// useful to users anyway because it includes model-load overhead).
type Stats struct {
	TokensPerSec float64
}

// New format (b9010+, single line on stdout when -st single-turn mode is used):
//
//	"[ Prompt: 280.3 t/s | Generation: 33.6 t/s ]"
//
// We capture the Generation rate; that's what the user cares about for inference.
var newFormatRe = regexp.MustCompile(`\[\s*Prompt:\s*[0-9.]+\s*t/s\s*\|\s*Generation:\s*([0-9.]+)\s*t/s\s*\]`)

// Legacy formats on stderr.
//
// Modern (b4000-ish to ~b9010): "llama_perf_context_print:        eval time =    7423.47 ms /   332 runs   (   22.36 ms per token,    44.72 tokens per second)"
// Older:                        "llama_print_timings:             eval time =    4178.32 ms /    31 runs   (   134.78 ms per token)"
//
// We capture both ms-per-token and (optional) tokens-per-second. The trailing
// `[^)]*?` between ms-per-token and the tokens-per-second pair tolerates
// future llama.cpp builds that interleave additional metrics inside the parens
// (e.g. GFLOP/s) — keeping the inline tokens-per-second authoritative rather
// than silently falling back to a computed-from-ms value when the format
// drifts. `[^)]` cannot cross either the closing paren or a newline.
var legacyEvalLineRe = regexp.MustCompile(
	`(?m)^llama_(?:perf_context_print|print_timings):\s*eval time\s*=\s*[0-9.]+\s*ms\s*/\s*\d+\s*runs?\s*\(\s*([0-9.]+)\s*ms per token(?:[^)]*?,\s*([0-9.]+)\s*tokens per second)?`,
)

// Parse extracts Stats from the combined stdout+stderr of an llama-cli run.
// Tries the b9010+ single-line format first, then falls back to the legacy
// llama_perf_*/llama_print_timings formats. Returns ErrParse if neither
// format matches.
func Parse(combined string) (Stats, error) {
	if m := newFormatRe.FindStringSubmatch(combined); m != nil {
		v, err := strconv.ParseFloat(m[1], 64)
		if err != nil {
			return Stats{}, fmt.Errorf("generation t/s value %q: %w", m[1], ErrParse)
		}
		return Stats{TokensPerSec: v}, nil
	}

	eval := legacyEvalLineRe.FindStringSubmatch(combined)
	if eval == nil {
		return Stats{}, fmt.Errorf("no recognisable tokens/sec line found: %w", ErrParse)
	}
	msPerTok, err := strconv.ParseFloat(eval[1], 64)
	if err != nil {
		return Stats{}, fmt.Errorf("ms-per-token value %q: %w", eval[1], ErrParse)
	}
	if eval[2] != "" {
		v, err := strconv.ParseFloat(eval[2], 64)
		if err != nil {
			return Stats{}, fmt.Errorf("tokens-per-second value %q: %w", eval[2], ErrParse)
		}
		return Stats{TokensPerSec: v}, nil
	}
	if msPerTok == 0 {
		return Stats{}, fmt.Errorf("ms-per-token is zero (cannot compute tokens-per-second): %w", ErrParse)
	}
	return Stats{TokensPerSec: 1000.0 / msPerTok}, nil
}
