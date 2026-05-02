// Package cli builds the cobra command tree and orchestrates llamavm flows.
// The package never imports its concrete dependencies — instead it consumes
// the narrow interfaces below, which the binary wires up at main.go.
package cli

import (
	"context"
	"io"
	"time"
)

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

// Deps collects everything the cli subcommands need.
type Deps struct {
	Store    Store
	GitHub   GitHubClient
	Builder  Builder
	Git      CommandRunner
	Platform Platform
	Stdout   io.Writer
	Stderr   io.Writer
	Now      func() time.Time
}
