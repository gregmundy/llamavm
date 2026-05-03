package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gregmundy/llamavm/internal/version"
)

// healthyDoctorDeps returns a Deps wired with stub implementations that make
// every doctor check pass. Individual tests below override one field at a
// time to drive a single check into the failing branch.
func healthyDoctorDeps(t *testing.T) *Deps {
	t.Helper()
	return &Deps{
		Store: &fakeStore{
			installed: []string{"b5046"},
			active:    "b5046",
			hasActive: true,
			shimsDir:  "/home/u/.llamavm/shims",
		},
		Resolver: &fakeResolver{tag: "b5046"},
		Getwd:    func() (string, error) { return "/work", nil },
		Getenv: func(key string) string {
			if key == "PATH" {
				return "/usr/bin:/home/u/.llamavm/shims:/opt/local/bin"
			}
			return ""
		},
		LookPath: func(name string) (string, error) {
			switch name {
			case "cmake":
				return "/opt/homebrew/bin/cmake", nil
			case "git":
				return "/usr/bin/git", nil
			case "llama-cli":
				return "/home/u/.llamavm/shims/llama-cli", nil
			}
			return "", errors.New("not found")
		},
		XcodeSelectPath: func(ctx context.Context) (string, error) {
			return "/Applications/Xcode.app/Contents/Developer", nil
		},
	}
}

func TestDoctor_AllChecksPass(t *testing.T) {
	deps, _ := runDoctorWithFakeShims(t, true)
	out, _, err := runRoot(t, deps, "doctor")
	if err != nil {
		t.Fatalf("doctor: %v\noutput:\n%s", err, out)
	}
	passes := strings.Count(out, "✓")
	if passes != 8 {
		t.Fatalf("got %d ✓ markers, want 8\noutput:\n%s", passes, out)
	}
	if strings.Contains(out, "✗") {
		t.Fatalf("expected no ✗ markers in healthy output:\n%s", out)
	}
	// Anchor to the trailing line so a stray "OK" substring in some future
	// check label can't satisfy this assertion.
	if !strings.HasSuffix(strings.TrimRight(out, "\n"), "\nOK") {
		t.Fatalf("expected output to end with a line equal to OK, got:\n%s", out)
	}
}

// runDoctorWithFakeShims creates a temp shims dir AND a parallel version
// dir (so checkShimFiles can resolve each shim to a real on-disk binary
// in the active version's bin/). When populate is true, both shims and
// binaries are written; when false, neither is.
func runDoctorWithFakeShims(t *testing.T, populate bool) (*Deps, string) {
	t.Helper()
	root := t.TempDir()
	shimsDir := filepath.Join(root, ".llamavm", "shims")
	versionDir := filepath.Join(root, ".llamavm", "versions", "b5046")
	if populate {
		if err := os.MkdirAll(shimsDir, 0o755); err != nil {
			t.Fatal(err)
		}
		versionBin := filepath.Join(versionDir, "bin")
		if err := os.MkdirAll(versionBin, 0o755); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"llama-cli", "llama-server", "llama-quantize"} {
			if err := os.WriteFile(filepath.Join(shimsDir, name), []byte("#!/bin/sh\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			// And the corresponding binary in the active version's bin so
			// checkShimFiles' resolution lookup succeeds.
			if err := os.WriteFile(filepath.Join(versionBin, name), []byte("#!/bin/sh\n"), 0o755); err != nil {
				t.Fatal(err)
			}
		}
	}
	deps := healthyDoctorDeps(t)
	deps.Store = &fakeStore{
		installed:    []string{"b5046"},
		active:       "b5046",
		hasActive:    true,
		shimsDir:     shimsDir,
		versionDirFn: func(_ string) string { return versionDir },
	}
	deps.Getenv = func(key string) string {
		if key == "PATH" {
			return "/usr/bin:" + shimsDir + ":/opt/local/bin"
		}
		return ""
	}
	return deps, shimsDir
}

func TestDoctor_RootMissing(t *testing.T) {
	deps, _ := runDoctorWithFakeShims(t, false)
	deps.Store = &fakeStore{
		installed: []string{"b5046"},
		active:    "b5046",
		hasActive: true,
		shimsDir:  "/nonexistent/.llamavm/shims",
	}
	out, _, err := runRoot(t, deps, "doctor")
	if err == nil {
		t.Fatal("expected non-nil err when root missing")
	}
	if !errors.Is(err, ErrUserError) {
		t.Fatalf("err = %v, want ErrUserError chain", err)
	}
	if !strings.Contains(out, "✗ ~/.llamavm directory exists") {
		t.Fatalf("expected root-missing fail line, got:\n%s", out)
	}
}

func TestDoctor_ShimsDirMissingShimFiles(t *testing.T) {
	deps, _ := runDoctorWithFakeShims(t, false)
	out, _, err := runRoot(t, deps, "doctor")
	if err == nil {
		t.Fatal("expected non-nil err when shim files missing")
	}
	if !strings.Contains(out, "✗ every ~/.llamavm/shims/llama-* shim resolves") {
		t.Fatalf("expected shim-resolution fail line, got:\n%s", out)
	}
	if !strings.Contains(out, "llamavm install") {
		t.Fatalf("remediation should mention 'llamavm install', got:\n%s", out)
	}
}

func TestDoctor_ShimsNotOnPATH(t *testing.T) {
	deps, _ := runDoctorWithFakeShims(t, true)
	deps.Getenv = func(key string) string {
		if key == "PATH" {
			return "/usr/bin:/opt/local/bin"
		}
		return ""
	}
	out, _, err := runRoot(t, deps, "doctor")
	if err == nil {
		t.Fatal("expected non-nil err when shims not on PATH")
	}
	if !strings.Contains(out, "✗ ~/.llamavm/shims is on PATH") {
		t.Fatalf("expected PATH fail line, got:\n%s", out)
	}
	if !strings.Contains(out, `export PATH="$HOME/.llamavm/shims:$PATH"`) {
		t.Fatalf("expected exact PATH-export remediation, got:\n%s", out)
	}
}

func TestDoctor_NoVersionsInstalled(t *testing.T) {
	deps, _ := runDoctorWithFakeShims(t, true)
	deps.Store.(*fakeStore).installed = nil
	deps.Store.(*fakeStore).hasActive = false
	deps.Resolver = &fakeResolver{err: version.ErrNoActiveVersion}
	out, _, err := runRoot(t, deps, "doctor")
	if err == nil {
		t.Fatal("expected error when no versions installed")
	}
	if !strings.Contains(out, "✗ at least one version is installed") {
		t.Fatalf("expected versions fail line, got:\n%s", out)
	}
}

func TestDoctor_ActiveVersionUnresolved(t *testing.T) {
	deps, _ := runDoctorWithFakeShims(t, true)
	deps.Resolver = &fakeResolver{err: version.ErrNoActiveVersion}
	deps.Store.(*fakeStore).hasActive = false
	deps.Store.(*fakeStore).active = ""
	out, _, err := runRoot(t, deps, "doctor")
	if err == nil {
		t.Fatal("expected error when active version unresolved")
	}
	if !strings.Contains(out, "✗ active version resolves") {
		t.Fatalf("expected active-version fail line, got:\n%s", out)
	}
}

func TestDoctor_ActiveVersionPointsToUninstalled(t *testing.T) {
	deps, _ := runDoctorWithFakeShims(t, true)
	deps.Resolver = &fakeResolver{tag: "b9999"} // not in installed list
	out, _, err := runRoot(t, deps, "doctor")
	if err == nil {
		t.Fatal("expected error when active version not installed")
	}
	if !strings.Contains(out, "✗ active version resolves") {
		t.Fatalf("expected fail line for stale-active, got:\n%s", out)
	}
}

func TestDoctor_CmakeMissing(t *testing.T) {
	deps, _ := runDoctorWithFakeShims(t, true)
	deps.LookPath = func(name string) (string, error) {
		if name == "cmake" {
			return "", errors.New("not found")
		}
		return "/usr/bin/" + name, nil
	}
	out, _, err := runRoot(t, deps, "doctor")
	if err == nil {
		t.Fatal("expected error when cmake missing")
	}
	if !strings.Contains(out, "✗ cmake is on PATH") {
		t.Fatalf("expected cmake fail line, got:\n%s", out)
	}
	if !strings.Contains(out, "brew install cmake") {
		t.Fatalf("expected brew install cmake remediation, got:\n%s", out)
	}
}

func TestDoctor_GitMissing(t *testing.T) {
	deps, _ := runDoctorWithFakeShims(t, true)
	deps.LookPath = func(name string) (string, error) {
		if name == "git" {
			return "", errors.New("not found")
		}
		return "/usr/bin/" + name, nil
	}
	out, _, err := runRoot(t, deps, "doctor")
	if err == nil {
		t.Fatal("expected error when git missing")
	}
	if !strings.Contains(out, "✗ git is on PATH") {
		t.Fatalf("expected git fail line, got:\n%s", out)
	}
}

func TestDoctor_XcodeCLTMissing(t *testing.T) {
	deps, _ := runDoctorWithFakeShims(t, true)
	deps.XcodeSelectPath = func(ctx context.Context) (string, error) {
		return "", errors.New("xcode-select: error: unable to get active developer directory")
	}
	out, _, err := runRoot(t, deps, "doctor")
	if err == nil {
		t.Fatal("expected error when xcode-select fails")
	}
	if !strings.Contains(out, "✗ Xcode Command Line Tools are installed") {
		t.Fatalf("expected xcode CLT fail line, got:\n%s", out)
	}
	if !strings.Contains(out, "xcode-select --install") {
		t.Fatalf("expected xcode-select --install remediation, got:\n%s", out)
	}
}
