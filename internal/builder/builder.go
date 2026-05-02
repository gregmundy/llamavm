// Package builder runs the cmake configure + build sequence and exposes the
// host-platform facts the install flow uses to pass -j N to cmake.
package builder

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strconv"
)

// CommandRunner abstracts os/exec for testability.
type CommandRunner interface {
	Run(ctx context.Context, name string, args []string, dir string, stdout, stderr io.Writer) error
}

// ExecRunner is the production CommandRunner backed by os/exec.
type ExecRunner struct{}

// Run invokes name with args in dir, tee-ing stdout/stderr to the supplied writers.
func (ExecRunner) Run(ctx context.Context, name string, args []string, dir string, stdout, stderr io.Writer) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}

// Builder runs cmake configure + build inside a llama.cpp source tree.
type Builder struct {
	Runner   CommandRunner
	Platform Platform
}

// Build runs cmake configure and then cmake --build inside srcDir. Both
// cmake invocations write combined stdout+stderr to logWriter so callers can
// preserve build output on failure.
func (b *Builder) Build(ctx context.Context, srcDir string, logWriter io.Writer) error {
	configure := []string{"-B", "build", "-DGGML_METAL=ON", "-DCMAKE_BUILD_TYPE=Release"}
	if err := b.Runner.Run(ctx, "cmake", configure, srcDir, logWriter, logWriter); err != nil {
		return fmt.Errorf("cmake configure: %w", err)
	}
	build := []string{"--build", "build", "--config", "Release", "-j", strconv.Itoa(b.Platform.Cores())}
	if err := b.Runner.Run(ctx, "cmake", build, srcDir, logWriter, logWriter); err != nil {
		return fmt.Errorf("cmake build: %w", err)
	}
	return nil
}
