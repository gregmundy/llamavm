// Package cli builds the cobra command tree and orchestrates llamavm flows.
// The package never imports its concrete dependencies — instead it consumes
// the narrow interfaces below, which the binary wires up at main.go.
package cli

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/gregmundy/llamavm/internal/bench"
)

// ErrUserError marks errors caused by user input (version not installed, version
// not found upstream, etc.). main.go translates these to PRD §6.4 exit code 2.
var ErrUserError = errors.New("user error")

// Store is the version store contract used by the cli package.
type Store interface {
	IsInstalled(tag string) bool
	List() ([]string, error)
	Active() (string, error)
	SetActive(tag string) error
	ClearActive() error
	Remove(tag string) error
	VersionDir(tag string) string
	StagingDir(tag string) string
	PromoteStaging(tag string) error
	RemoveStaging(tag string) error
	LogsDir() string
	ShimsDir() string
	BenchmarksDir() string
}

// GitHubClient resolves and validates llama.cpp release tags.
type GitHubClient interface {
	Latest(ctx context.Context) (string, error)
	TagExists(ctx context.Context, tag string) error
}

// Builder runs the cmake configure + build sequence in a source tree.
type Builder interface {
	Build(ctx context.Context, srcDir string, logWriter io.Writer) error
}

// CommandRunner runs an external command. Used for `git clone` in install.
type CommandRunner interface {
	Run(ctx context.Context, name string, args []string, dir string, stdout, stderr io.Writer) error
}

// Platform answers host-environment questions the install flow cares about.
type Platform interface {
	IsAppleSilicon() bool
}

// Resolver returns the active tag for the given working directory or
// version.ErrNoActiveVersion. Pass "" when no cwd is available — the
// resolver will then consult only ~/.llamavm/current.
type Resolver interface {
	Resolve(cwd string) (string, error)
}

// ShimInstaller writes the three shim binaries into the shims directory.
// Implementations must be idempotent: calling EnsureInstalled twice with
// the same shimsDir is a no-op the second time.
type ShimInstaller interface {
	EnsureInstalled(shimsDir string) error
}

// Benchmarker runs a single bench against a model. Implementations honor
// useCache by returning a cached Result without executing when one exists.
type Benchmarker interface {
	Run(ctx context.Context, tag, modelPath string, useCache bool) (bench.Result, error)
}

// Deps collects everything the cli subcommands need.
type Deps struct {
	Store         Store
	GitHub        GitHubClient
	Builder       Builder
	Git           CommandRunner
	Platform      Platform
	Resolver      Resolver
	ShimInstaller ShimInstaller
	Benchmarker   Benchmarker
	Stdout        io.Writer
	Stderr        io.Writer
	Now           func() time.Time
	Getwd         func() (string, error)

	// LookPath wraps exec.LookPath. Doctor uses it to verify cmake and git
	// are reachable, and to confirm a llama-* shim resolves to ~/.llamavm/shims.
	LookPath func(name string) (string, error)
	// Getenv wraps os.Getenv. Doctor reads PATH from it.
	Getenv func(key string) string
	// XcodeSelectPath runs `xcode-select -p` and returns its trimmed stdout.
	// Used by doctor to verify Xcode CLT is installed.
	XcodeSelectPath func(ctx context.Context) (string, error)
}
