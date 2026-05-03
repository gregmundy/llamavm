package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/gregmundy/llamavm/internal/version"
)

func newInfoCmd(deps *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "info [version]",
		Short: "Show details about an installed llama.cpp version",
		Long: "Without arguments, shows details about the active version (the " +
			"one a shim invocation in the current directory would dispatch to). " +
			"With a tag argument, shows details about that specific installed " +
			"version.\n\nFields:\n" +
			"  Tag    - llamavm release tag (e.g. b9010)\n" +
			"  Source - where the tag was resolved from (.llama-version pin or\n" +
			"           ~/.llamavm/current); 'explicit' when a tag arg was given\n" +
			"  Build  - llama.cpp git SHA the binary was built from\n" +
			"  Path   - install directory\n" +
			"  Built  - install timestamp",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInfo(cmd.Context(), deps, args)
		},
	}
}

func runInfo(ctx context.Context, deps *Deps, args []string) error {
	tag, source, err := resolveInfoTarget(deps, args)
	if err != nil {
		return err
	}
	if !deps.Store.IsInstalled(tag) {
		return fmt.Errorf("%s is not installed; run 'llamavm install %s' first: %w", tag, tag, ErrUserError)
	}
	versionDir := deps.Store.VersionDir(tag)

	sha := readBuildSHA(ctx, deps, versionDir)
	built := readInstallTime(versionDir)

	fmt.Fprintf(deps.Stdout, "Tag:    %s\n", tag)
	fmt.Fprintf(deps.Stdout, "Source: %s\n", source)
	fmt.Fprintf(deps.Stdout, "Build:  %s (llama.cpp git SHA)\n", sha)
	fmt.Fprintf(deps.Stdout, "Path:   %s\n", versionDir)
	fmt.Fprintf(deps.Stdout, "Built:  %s\n", built)
	return nil
}

// resolveInfoTarget returns (tag, source-description). Source is the
// human-readable explanation: "pinned at <path>", "from <path>", or "explicit"
// when the tag was passed as an argument.
func resolveInfoTarget(deps *Deps, args []string) (string, string, error) {
	if len(args) == 1 {
		return args[0], "explicit (tag passed as argument)", nil
	}
	cwd, err := deps.Getwd()
	if err != nil {
		return "", "", fmt.Errorf("get working directory: %w", err)
	}
	res, err := deps.Resolver.ResolveDetailed(cwd)
	if err != nil {
		if errors.Is(err, version.ErrNoActiveVersion) {
			return "", "", fmt.Errorf("no active version: %w", ErrUserError)
		}
		return "", "", fmt.Errorf("resolve active version: %w", err)
	}
	switch res.Source {
	case version.SourcePin:
		return res.Tag, "pinned at " + res.Path, nil
	case version.SourceCurrent:
		return res.Tag, "from " + res.Path, nil
	default:
		return res.Tag, "(unknown)", nil
	}
}

// readBuildSHA returns the abbreviated git SHA the binary was built from, or
// "(unknown)" if the source dir is missing or git can't run.
func readBuildSHA(ctx context.Context, deps *Deps, versionDir string) string {
	sourceDir := filepath.Join(versionDir, "source")
	if _, err := os.Stat(sourceDir); err != nil {
		return "(unknown)"
	}
	var stdout bytes.Buffer
	if err := deps.Git.Run(ctx, "git",
		[]string{"rev-parse", "--short", "HEAD"},
		sourceDir, &stdout, io.Discard); err != nil {
		return "(unknown)"
	}
	sha := strings.TrimSpace(stdout.String())
	if sha == "" {
		return "(unknown)"
	}
	return sha
}

// readInstallTime returns the version directory's mtime as a formatted
// timestamp, or "(unknown)" on stat failure. The directory's mtime is set
// by PromoteStaging's rename — i.e. the install completion time.
func readInstallTime(versionDir string) string {
	info, err := os.Stat(versionDir)
	if err != nil {
		return "(unknown)"
	}
	return info.ModTime().Format("2006-01-02 15:04 MST")
}
