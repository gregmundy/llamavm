package bench

import (
	"errors"
	"math"
	"testing"
)

const llamaBenchStdout = `| model                          |       size |     params | backend    | threads |            test |                  t/s |
| ------------------------------ | ---------: | ---------: | ---------- | ------: | --------------: | -------------------: |
| gemma4 E4B Q4_K - Medium       |   4.62 GiB |     7.52 B | MTL,BLAS   |       4 |           pp256 |       1051.46 ± 0.00 |
| gemma4 E4B Q4_K - Medium       |   4.62 GiB |     7.52 B | MTL,BLAS   |       4 |           tg128 |         35.24 ± 0.00 |

build: d05fe1d (1)
`

func nearly(a, b, eps float64) bool { return math.Abs(a-b) <= eps }

func TestParse_LlamaBenchTgRow(t *testing.T) {
	got, err := Parse(llamaBenchStdout)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !nearly(got.TokensPerSec, 35.24, 0.01) {
		t.Fatalf("TokensPerSec = %v, want ~35.24 (tg128 row)", got.TokensPerSec)
	}
}

func TestParse_PicksTgRowNotPpRow(t *testing.T) {
	// pp256 row is 1051.46 t/s (much faster — prompt processing).
	// tg128 is 35.24 (token generation — what we want).
	got, err := Parse(llamaBenchStdout)
	if err != nil {
		t.Fatal(err)
	}
	if got.TokensPerSec > 100 {
		t.Fatalf("TokensPerSec = %v, parser picked the pp row by mistake", got.TokensPerSec)
	}
}

func TestParse_ToleratesNonZeroStdDev(t *testing.T) {
	// With -r >1, the ± value is non-zero; parser must still extract t/s.
	stdout := `| m | s | p | b | t |  test |     t/s |
| - | - | - | - | - | ----: | ------: |
| x | 1 | 1 | x | 1 | tg128 | 42.5 ± 1.2 |
`
	got, err := Parse(stdout)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !nearly(got.TokensPerSec, 42.5, 0.01) {
		t.Fatalf("TokensPerSec = %v, want 42.5", got.TokensPerSec)
	}
}

func TestParse_DifferentTgN(t *testing.T) {
	// tg<N> for any N (tg64, tg256, tg512) — regex must not pin the count.
	stdout := `| x | 1 | 1 | x | 1 | tg512 |  29.1 ± 0.0 |`
	got, err := Parse(stdout)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !nearly(got.TokensPerSec, 29.1, 0.01) {
		t.Fatalf("TokensPerSec = %v, want 29.1", got.TokensPerSec)
	}
}

func TestParse_EmptyInputIsErrParse(t *testing.T) {
	if _, err := Parse(""); !errors.Is(err, ErrParse) {
		t.Fatalf("Parse(\"\"): got %v, want ErrParse", err)
	}
}

func TestParse_NoTgRowIsErrParse(t *testing.T) {
	stdout := `| x | 1 | 1 | x | 1 | pp256 | 1000 ± 0 |
build: abc (1)
`
	if _, err := Parse(stdout); !errors.Is(err, ErrParse) {
		t.Fatalf("Parse(no tg row): got %v, want ErrParse", err)
	}
}
