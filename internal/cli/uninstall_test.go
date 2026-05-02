package cli

import (
	"strings"
	"testing"
)

func TestUninstall_RequiresVersionArg(t *testing.T) {
	deps := &Deps{Store: &fakeStore{}}
	if _, _, err := runRoot(t, deps, "uninstall"); err == nil {
		t.Fatal("expected error when version arg missing")
	}
}

func TestUninstall_NotInstalled(t *testing.T) {
	store := &fakeStore{}
	deps := &Deps{Store: store}
	_, errBuf, err := runRoot(t, deps, "uninstall", "b5046")
	if err == nil {
		t.Fatal("expected error when version not installed")
	}
	if !strings.Contains(err.Error(), "not installed") && !strings.Contains(errBuf, "not installed") {
		t.Fatalf("err/stderr should mention not installed; err=%v stderr=%q", err, errBuf)
	}
}

func TestUninstall_RemovesAndClearsActive(t *testing.T) {
	store := &fakeStore{
		installed: []string{"b5046"},
		active:    "b5046",
		hasActive: true,
	}
	deps := &Deps{Store: store}
	out, _, err := runRoot(t, deps, "uninstall", "b5046")
	if err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if len(store.removed) != 1 || store.removed[0] != "b5046" {
		t.Fatalf("expected b5046 removed, removed=%v", store.removed)
	}
	if !store.clearActive {
		t.Fatal("expected active to be cleared")
	}
	if !strings.Contains(out, "Uninstalled b5046") {
		t.Fatalf("stdout = %q, want 'Uninstalled b5046'", out)
	}
}

func TestUninstall_KeepsActiveIfDifferent(t *testing.T) {
	store := &fakeStore{
		installed: []string{"b5046", "b5489"},
		active:    "b5489",
		hasActive: true,
	}
	deps := &Deps{Store: store}
	if _, _, err := runRoot(t, deps, "uninstall", "b5046"); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if store.clearActive {
		t.Fatal("expected active untouched when removing a non-active version")
	}
}
