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
	if !strings.Contains(combined, "no active version") {
		t.Fatalf("output = %q, want it to mention 'no active version'", combined)
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

func TestCurrent_VerboseShowsPinSource(t *testing.T) {
	res := &fakeResolver{
		tag:        "b5046",
		source:     version.SourcePin,
		sourcePath: "/work/project/.llama-version",
	}
	deps := &Deps{
		Store:    &fakeStore{},
		Resolver: res,
		Getwd:    func() (string, error) { return "/work/project", nil },
	}
	out, _, err := runRoot(t, deps, "current", "-v")
	if err != nil {
		t.Fatalf("current -v: %v", err)
	}
	want := "b5046 (pinned at /work/project/.llama-version)\n"
	if out != want {
		t.Fatalf("stdout = %q, want %q", out, want)
	}
}

func TestCurrent_VerboseShowsCurrentFileSource(t *testing.T) {
	res := &fakeResolver{
		tag:        "b5046",
		source:     version.SourceCurrent,
		sourcePath: "/home/u/.llamavm/current",
	}
	deps := &Deps{
		Store:    &fakeStore{},
		Resolver: res,
		Getwd:    func() (string, error) { return "/work/project", nil },
	}
	out, _, err := runRoot(t, deps, "current", "--verbose")
	if err != nil {
		t.Fatalf("current --verbose: %v", err)
	}
	want := "b5046 (from /home/u/.llamavm/current)\n"
	if out != want {
		t.Fatalf("stdout = %q, want %q", out, want)
	}
}

func TestCurrent_NoVerboseStillJustTheTag(t *testing.T) {
	// Regression: -v defaults off; bare `current` keeps its scriptable
	// one-line output.
	res := &fakeResolver{
		tag:        "b5046",
		source:     version.SourcePin,
		sourcePath: "/work/.llama-version",
	}
	deps := &Deps{
		Store:    &fakeStore{},
		Resolver: res,
		Getwd:    func() (string, error) { return "/work", nil },
	}
	out, _, err := runRoot(t, deps, "current")
	if err != nil {
		t.Fatalf("current: %v", err)
	}
	if out != "b5046\n" {
		t.Fatalf("stdout = %q, want plain tag (no source) when -v is off", out)
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
