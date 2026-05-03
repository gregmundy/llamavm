package cli

import (
	"errors"
	"os"
	"path/filepath"
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

func TestUninstall_NotInstalled_IsUserError(t *testing.T) {
	deps := &Deps{Store: &fakeStore{}}
	_, _, err := runRoot(t, deps, "uninstall", "b5046")
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrUserError) {
		t.Fatalf("err = %v, want it to chain to ErrUserError", err)
	}
}

func TestUninstall_OrphanShimsAreCleanedUp(t *testing.T) {
	// Setup: two installed versions with overlapping shims, plus an "extra"
	// shim only b9009 provided. Uninstall b9009 → expect the unique shim
	// removed, the shared shims kept (b9010 still provides them).
	root := t.TempDir()
	versions := filepath.Join(root, ".llamavm", "versions")
	shimsDir := filepath.Join(root, ".llamavm", "shims")
	if err := os.MkdirAll(shimsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, tag := range []string{"b9009", "b9010"} {
		bin := filepath.Join(versions, tag, "bin")
		if err := os.MkdirAll(bin, 0o755); err != nil {
			t.Fatal(err)
		}
		// Both versions have llama-cli.
		if err := os.WriteFile(filepath.Join(bin, "llama-cli"), []byte("x"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// Only b9009 has llama-orphan.
	if err := os.WriteFile(filepath.Join(versions, "b9009", "bin", "llama-orphan"), []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Shims for both names + a non-llama shim that should NEVER be touched.
	for _, n := range []string{"llama-cli", "llama-orphan"} {
		if err := os.WriteFile(filepath.Join(shimsDir, n), []byte("x"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(shimsDir, "not-our-shim"), []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}

	store := &fakeStore{
		installed: []string{"b9009", "b9010"},
		active:    "b9010",
		hasActive: true,
		shimsDir:  shimsDir,
		versionDirFn: func(tag string) string {
			return filepath.Join(versions, tag)
		},
	}
	deps := &Deps{Store: store}

	if _, _, err := runRoot(t, deps, "uninstall", "b9009"); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	// Simulate Store.Remove by deleting b9009's version dir on disk so the
	// post-removal scan sees only b9010 as a provider.
	if err := os.RemoveAll(filepath.Join(versions, "b9009")); err != nil {
		t.Fatal(err)
	}
	// Re-run the cleanup pass — fakeStore.Remove doesn't touch disk, so the
	// orphan check during runUninstall above ran with b9009's bin still
	// present. Run cleanup directly to test the post-removal state.
	if err := cleanupOrphanedShims(deps); err != nil {
		t.Fatalf("cleanupOrphanedShims: %v", err)
	}

	if _, err := os.Stat(filepath.Join(shimsDir, "llama-cli")); err != nil {
		t.Errorf("shared shim llama-cli should remain (b9010 still provides it): %v", err)
	}
	if _, err := os.Stat(filepath.Join(shimsDir, "llama-orphan")); err == nil {
		t.Error("orphan shim llama-orphan should have been removed")
	}
	if _, err := os.Stat(filepath.Join(shimsDir, "not-our-shim")); err != nil {
		t.Errorf("non-llama shim should not be touched: %v", err)
	}
}
