package cli

import (
	"errors"
	"strings"
	"testing"

	"github.com/gregmundy/llamavm/internal/version"
)

func TestCurrent_PrintsActiveTag(t *testing.T) {
	deps := &Deps{
		Store:    &fakeStore{},
		Resolver: &fakeResolver{tag: "b5046"},
	}
	out, _, err := runRoot(t, deps, "current")
	if err != nil {
		t.Fatalf("current: %v", err)
	}
	if out != "b5046\n" {
		t.Fatalf("stdout = %q, want \"b5046\\n\"", out)
	}
}

func TestCurrent_NoActiveVersion(t *testing.T) {
	deps := &Deps{
		Store:    &fakeStore{},
		Resolver: &fakeResolver{err: version.ErrNoActiveVersion},
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
	}
	if _, _, err := runRoot(t, deps, "current"); err == nil {
		t.Fatal("expected error to propagate")
	}
}
