package cli

import (
	"errors"
	"strings"
	"testing"

	"github.com/gregmundy/llamavm/internal/version"
)

func TestCurrent_PrintsActiveTag(t *testing.T) {
	res := &fakeResolver{tag: "b5046"}
	deps := &Deps{
		Store:    &fakeStore{},
		Resolver: res,
		Getwd:    func() (string, error) { return "/work/project", nil },
	}
	out, _, err := runRoot(t, deps, "current")
	if err != nil {
		t.Fatalf("current: %v", err)
	}
	if out != "b5046\n" {
		t.Fatalf("stdout = %q, want \"b5046\\n\"", out)
	}
	if res.lastCwd != "/work/project" {
		t.Fatalf("Resolver.Resolve called with cwd=%q, want \"/work/project\"", res.lastCwd)
	}
}

func TestCurrent_NoActiveVersion(t *testing.T) {
	deps := &Deps{
		Store:    &fakeStore{},
		Resolver: &fakeResolver{err: version.ErrNoActiveVersion},
		Getwd:    func() (string, error) { return "/work/project", nil },
	}
	_, errOut, err := runRoot(t, deps, "current")
	if err == nil {
		t.Fatal("expected error when no active version")
	}
	if !errors.Is(err, ErrUserError) {
		t.Fatalf("err = %v, want chained ErrUserError", err)
	}
	combined := errOut + err.Error()
	if !strings.Contains(combined, "No active version") {
		t.Fatalf("output = %q, want it to mention 'No active version'", combined)
	}
}

func TestCurrent_PropagatesUnexpectedError(t *testing.T) {
	deps := &Deps{
		Store:    &fakeStore{},
		Resolver: &fakeResolver{err: errors.New("disk on fire")},
		Getwd:    func() (string, error) { return "/work/project", nil },
	}
	if _, _, err := runRoot(t, deps, "current"); err == nil {
		t.Fatal("expected error to propagate")
	}
}

func TestCurrent_GetwdErrorPropagates(t *testing.T) {
	deps := &Deps{
		Store:    &fakeStore{},
		Resolver: &fakeResolver{tag: "b5046"},
		Getwd:    func() (string, error) { return "", errors.New("getwd failed") },
	}
	_, _, err := runRoot(t, deps, "current")
	if err == nil {
		t.Fatal("expected error when Getwd fails")
	}
	if errors.Is(err, ErrUserError) {
		t.Fatalf("Getwd failure should not be a user error, got %v", err)
	}
}
