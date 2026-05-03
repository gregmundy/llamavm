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

// Stats is the parsed throughput info from a single llama-bench run. Wall-clock
// total time is measured by the Runner separately.
type Stats struct {
	TokensPerSec float64
}

// llama-bench markdown output looks like:
//
//	| model       |  size | params | backend  | threads |  test |        t/s |
//	| ----------- | ----: | -----: | -------- | ------: | ----: | ---------: |
//	| gemma4 E4B  | 4.62  |  7.52B | MTL,BLAS |       4 | pp256 | 1051.46 ±0 |
//	| gemma4 E4B  | 4.62  |  7.52B | MTL,BLAS |       4 | tg128 |  35.24 ±0 |
//
// We capture the tg<N> row's t/s — that's the token-generation rate users
// feel during inference. pp<N> (prompt processing) is faster and less
// representative of perceived speed.
var tgRowRe = regexp.MustCompile(`\|\s*tg\d+\s*\|\s*([0-9.]+)\s*±`)

// Parse extracts Stats from the combined stdout+stderr of an llama-bench
// run. Returns ErrParse if no tg<N> row is present.
func Parse(combined string) (Stats, error) {
	m := tgRowRe.FindStringSubmatch(combined)
	if m == nil {
		return Stats{}, fmt.Errorf("no tg<N> row found in llama-bench output: %w", ErrParse)
	}
	v, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return Stats{}, fmt.Errorf("tg t/s value %q: %w", m[1], ErrParse)
	}
	return Stats{TokensPerSec: v}, nil
}
