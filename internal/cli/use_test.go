package cli

import (
	"errors"
	"strings"
	"testing"
)

func TestUse_RequiresVersion(t *testing.T) {
	deps := &Deps{Store: &fakeStore{}}
	if _, _, err := runRoot(t, deps, "use"); err == nil {
		t.Fatal("expected error when version arg missing")
	}
}

func TestUse_HappyPath(t *testing.T) {
	store := &fakeStore{installed: []string{"b5046"}}
	deps := &Deps{Store: store}
	out, _, err := runRoot(t, deps, "use", "b5046")
	if err != nil {
		t.Fatalf("use: %v", err)
	}
	if !store.hasActive || store.active != "b5046" {
		t.Fatalf("active not set: hasActive=%v active=%q", store.hasActive, store.active)
	}
	if !strings.Contains(out, "b5046") {
		t.Fatalf("stdout = %q, want it to mention b5046", out)
	}
}

func TestUse_NotInstalled_IsUserError(t *testing.T) {
	store := &fakeStore{installed: []string{"b5489"}}
	deps := &Deps{Store: store}
	_, _, err := runRoot(t, deps, "use", "b5046")
	if err == nil {
		t.Fatal("expected error when version not installed")
	}
	if !errors.Is(err, ErrUserError) {
		t.Fatalf("err = %v, want chained ErrUserError", err)
	}
	if !strings.Contains(err.Error(), "llamavm install") {
		t.Fatalf("err = %v, want it to suggest 'llamavm install'", err)
	}
	if store.hasActive {
		t.Fatal("active should not be set when version is not installed")
	}
}

func TestUse_Latest_PicksHighestNumeric(t *testing.T) {
	// b10000 > b9999 numerically but lex-sorts after b1... — verify the
	// resolution sorts numerically for b<digits> tags so this future
	// crossover doesn't pick the wrong version.
	store := &fakeStore{installed: []string{"b9999", "b10000", "b5046"}}
	deps := &Deps{Store: store}
	out, _, err := runRoot(t, deps, "use", "latest")
	if err != nil {
		t.Fatalf("use latest: %v", err)
	}
	if store.active != "b10000" {
		t.Fatalf("active = %q, want b10000", store.active)
	}
	if !strings.Contains(out, "b10000") {
		t.Fatalf("stdout = %q, want it to mention b10000", out)
	}
}

func TestUse_Latest_SingleInstalled(t *testing.T) {
	store := &fakeStore{installed: []string{"b5046"}}
	deps := &Deps{Store: store}
	if _, _, err := runRoot(t, deps, "use", "latest"); err != nil {
		t.Fatalf("use latest: %v", err)
	}
	if store.active != "b5046" {
		t.Fatalf("active = %q, want b5046", store.active)
	}
}

func TestUse_Latest_NoneInstalled(t *testing.T) {
	store := &fakeStore{installed: nil}
	deps := &Deps{Store: store}
	_, _, err := runRoot(t, deps, "use", "latest")
	if err == nil {
		t.Fatal("expected error when no versions installed")
	}
	if !errors.Is(err, ErrUserError) {
		t.Fatalf("err = %v, want chained ErrUserError", err)
	}
	if !strings.Contains(err.Error(), "llamavm install") {
		t.Fatalf("err = %v, want it to suggest 'llamavm install'", err)
	}
	if store.hasActive {
		t.Fatal("active should not be set when nothing is installed")
	}
}
