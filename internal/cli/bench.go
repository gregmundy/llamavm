package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/gregmundy/llamavm/internal/bench"
)

func newBenchCmd(deps *Deps) *cobra.Command {
	var modelPath string
	var noCache bool
	cmd := &cobra.Command{
		Use:   "bench <version|all>",
		Short: "Benchmark an installed llama.cpp version against a model",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if args[0] == "all" {
				return runBenchAll(cmd.Context(), deps, modelPath, !noCache)
			}
			return runBenchSingle(cmd.Context(), deps, args[0], modelPath, !noCache)
		},
	}
	cmd.Flags().StringVar(&modelPath, "model", "", "Path to a .gguf model file (required)")
	cmd.Flags().BoolVar(&noCache, "no-cache", false, "Bypass the result cache and re-run")
	_ = cmd.MarkFlagRequired("model")
	return cmd
}

func runBenchSingle(ctx context.Context, deps *Deps, tag, modelPath string, useCache bool) error {
	if !deps.Store.IsInstalled(tag) {
		return fmt.Errorf("%s is not installed; run 'llamavm install %s' first: %w", tag, tag, ErrUserError)
	}
	res, err := deps.Benchmarker.Run(ctx, tag, modelPath, useCache)
	if err != nil {
		if errors.Is(err, bench.ErrModelNotFound) {
			return fmt.Errorf("model %s not found: %w", modelPath, ErrUserError)
		}
		if errors.Is(err, bench.ErrBinaryNotFound) {
			return fmt.Errorf("llama-cli for %s not found: %w", tag, ErrUserError)
		}
		return fmt.Errorf("bench %s: %w", tag, err)
	}
	cachedSuffix := ""
	if res.Cached {
		cachedSuffix = " (cached)"
	}
	fmt.Fprintf(deps.Stdout, "%s: %.1f t/s, %.1fs%s\n",
		res.Version, res.TokensPerSec, res.TotalTimeSeconds, cachedSuffix)
	return nil
}

func runBenchAll(ctx context.Context, deps *Deps, modelPath string, useCache bool) error {
	tags, err := deps.Store.List()
	if err != nil {
		return fmt.Errorf("list installed: %w", err)
	}
	if len(tags) == 0 {
		return fmt.Errorf("no versions installed; run 'llamavm install latest' first: %w", ErrUserError)
	}
	active, _ := deps.Store.Active() // empty string when none — fine

	type row struct {
		tag string
		res bench.Result
		err error
	}
	rows := make([]row, 0, len(tags))
	for _, tag := range tags {
		res, err := deps.Benchmarker.Run(ctx, tag, modelPath, useCache)
		rows = append(rows, row{tag: tag, res: res, err: err})
	}

	// active's tokens/sec is the comparison baseline. If active failed or
	// no active is set, deltas are omitted.
	var baseline float64
	if active != "" {
		for _, r := range rows {
			if r.tag == active && r.err == nil {
				baseline = r.res.TokensPerSec
				break
			}
		}
	}

	// Always 4 columns: Version | Tokens/sec | Total Time | Status. The
	// Status column carries the "current"/"+X% vs current" marker when an
	// active version is set, and the "failed: ..." note for failed rows. It
	// stays empty otherwise.
	tw := tabwriter.NewWriter(deps.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(tw, "Version\tTokens/sec\tTotal Time\tStatus")

	var best row
	bestSet := false
	for _, r := range rows {
		var tps, tt, status string
		if r.err != nil {
			tps = "—"
			tt = "—"
			status = fmt.Sprintf("failed: %v", r.err)
		} else {
			tps = fmt.Sprintf("%.1f t/s", r.res.TokensPerSec)
			tt = fmt.Sprintf("%.1fs", r.res.TotalTimeSeconds)
			switch {
			case r.tag == active:
				status = "current"
			case baseline > 0:
				delta := (r.res.TokensPerSec - baseline) / baseline * 100
				sign := "+"
				if delta < 0 {
					sign = ""
				}
				status = fmt.Sprintf("%s%.1f%% vs current", sign, delta)
			}
		}
		fmt.Fprintln(tw, strings.Join([]string{r.tag, tps, tt, status}, "\t"))
		if r.err == nil && (!bestSet || r.res.TokensPerSec > best.res.TokensPerSec) {
			best = r
			bestSet = true
		}
	}
	tw.Flush()
	if bestSet {
		fmt.Fprintf(deps.Stdout, "\nBest: %s (%.1f t/s)\n", best.tag, best.res.TokensPerSec)
	}
	return nil
}
