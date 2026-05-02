package cli

import (
	"context"
	"errors"
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
	deps := healthyDoctorDeps(t)
	out, _, err := runRoot(t, deps, "doctor")
	if err != nil {
		t.Fatalf("doctor: %v", err)
	}
	// Eight check lines, each prefixed with the pass marker.
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
