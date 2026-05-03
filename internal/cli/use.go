package cli

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

func newUseCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "use <version>",
		Short: "Set the global active llama.cpp version",
		Long: "Set the global active llama.cpp version. Pass 'latest' to pick " +
			"the highest-numbered installed version (resolved locally — does not " +
			"contact GitHub).",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUse(deps, args[0])
		},
	}
}

func runUse(deps *Deps, tag string) error {
	if tag == "latest" {
		resolved, err := latestInstalled(deps.Store)
		if err != nil {
			return err
		}
		tag = resolved
	}
	if !deps.Store.IsInstalled(tag) {
		return fmt.Errorf("%s is not installed; run 'llamavm install %s' first: %w", tag, tag, ErrUserError)
	}
	if err := deps.Store.SetActive(tag); err != nil {
		return fmt.Errorf("set active: %w", err)
	}
	fmt.Fprintf(deps.Stdout, "Active version: %s\n", tag)
	return nil
}

// latestInstalled returns the highest-numbered installed tag. For tags
// matching b<digits> (the llama.cpp release convention) it sorts numerically
// so b10000 > b9999; mixed or non-conforming tags fall back to lexicographic
// order. Returns ErrUserError when no versions are installed.
func latestInstalled(store Store) (string, error) {
	tags, err := store.List()
	if err != nil {
		return "", fmt.Errorf("list versions: %w", err)
	}
	if len(tags) == 0 {
		return "", fmt.Errorf("no versions installed; run 'llamavm install latest' first: %w", ErrUserError)
	}
	sort.Slice(tags, func(i, j int) bool {
		return tagLess(tags[i], tags[j])
	})
	return tags[len(tags)-1], nil
}

// tagLess orders b<digits> tags numerically and falls back to lexicographic
// order when either operand doesn't match that shape.
func tagLess(a, b string) bool {
	ai, aok := parseLlamaTag(a)
	bi, bok := parseLlamaTag(b)
	if aok && bok {
		return ai < bi
	}
	return a < b
}

// parseLlamaTag extracts the integer suffix of a "b<digits>" tag. Returns
// (n, true) on success, (0, false) for any other shape.
func parseLlamaTag(s string) (int, bool) {
	if !strings.HasPrefix(s, "b") {
		return 0, false
	}
	n, err := strconv.Atoi(s[1:])
	if err != nil {
		return 0, false
	}
	return n, true
}
