package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	if !strings.Contains(out, "OK") {
		t.Fatalf("expected trailing OK summary, got:\n%s", out)
	}
}

// runDoctorWithFakeShims creates a temp shims dir, optionally populates it
// with the three expected shim files, and returns deps wired to it.
func runDoctorWithFakeShims(t *testing.T, populate bool) (*Deps, string) {
	t.Helper()
	root := t.TempDir()
	shimsDir := filepath.Join(root, ".llamavm", "shims")
	if populate {
		if err := os.MkdirAll(shimsDir, 0o755); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"llama-cli", "llama-server", "llama-quantize"} {
			if err := os.WriteFile(filepath.Join(shimsDir, name), []byte("#!/bin/sh\n"), 0o755); err != nil {
				t.Fatal(err)
			}
		}
	}
	deps := healthyDoctorDeps(t)
	deps.Store = &fakeStore{
		installed: []string{"b5046"},
		active:    "b5046",
		hasActive: true,
		shimsDir:  shimsDir,
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
	if !strings.Contains(out, "✗ ~/.llamavm/shims contains llama-cli, llama-server, llama-quantize") {
		t.Fatalf("expected shim-files fail line, got:\n%s", out)
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
