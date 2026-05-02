package cli

import (
	"context"
	"errors"
	"fmt"

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
				// Implemented in Task 5; placeholder for now. User-error
				// classification (exit 2) is correct: "feature not in this
				// build" is closer to bad input than to a system fault.
				return fmt.Errorf("'bench all' is not yet implemented: %w", ErrUserError)
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
