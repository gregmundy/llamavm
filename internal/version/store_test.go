package version

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	return New(t.TempDir())
}

func TestStore_PathsAndRoot(t *testing.T) {
	home := t.TempDir()
	s := New(home)
	if got, want := s.Root(), filepath.Join(home, ".llamavm"); got != want {
		t.Fatalf("Root = %q, want %q", got, want)
	}
	if got, want := s.VersionsDir(), filepath.Join(home, ".llamavm", "versions"); got != want {
		t.Fatalf("VersionsDir = %q, want %q", got, want)
	}
	if got, want := s.VersionDir("b5046"), filepath.Join(home, ".llamavm", "versions", "b5046"); got != want {
		t.Fatalf("VersionDir = %q, want %q", got, want)
	}
	if got, want := s.StagingDir("b5046"), filepath.Join(home, ".llamavm", "versions", ".staging-b5046"); got != want {
		t.Fatalf("StagingDir = %q, want %q", got, want)
	}
	if got, want := s.CurrentFile(), filepath.Join(home, ".llamavm", "current"); got != want {
		t.Fatalf("CurrentFile = %q, want %q", got, want)
	}
	if got, want := s.LogsDir(), filepath.Join(home, ".llamavm", "logs"); got != want {
		t.Fatalf("LogsDir = %q, want %q", got, want)
	}
	if got, want := s.ShimsDir(), filepath.Join(home, ".llamavm", "shims"); got != want {
		t.Fatalf("ShimsDir = %q, want %q", got, want)
	}
	if got, want := s.BenchmarksDir(), filepath.Join(home, ".llamavm", "benchmarks"); got != want {
		t.Fatalf("BenchmarksDir = %q, want %q", got, want)
	}
}

func TestStore_IsInstalled(t *testing.T) {
	s := newTestStore(t)
	if s.IsInstalled("b5046") {
		t.Fatal("expected not installed on empty store")
	}
	if err := os.MkdirAll(s.VersionDir("b5046"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !s.IsInstalled("b5046") {
		t.Fatal("expected installed after mkdir")
	}
}

func TestStore_List_HidesDotEntriesAndSorts(t *testing.T) {
	s := newTestStore(t)
	got, err := s.List()
	if err != nil {
		t.Fatalf("List on empty: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty, got %v", got)
	}

	for _, name := range []string{"b5489", "b5046", "b5400", ".staging-b9000"} {
		if err := os.MkdirAll(filepath.Join(s.VersionsDir(), name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	got, err = s.List()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"b5046", "b5400", "b5489"}
	if !sort.StringsAreSorted(got) {
		t.Fatalf("List not sorted: %v", got)
	}
	if len(got) != len(want) {
		t.Fatalf("List = %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("List[%d] = %q, want %q (full: %v)", i, got[i], want[i], got)
		}
	}
}

func TestStore_ActiveRoundTrip(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.Active(); !errors.Is(err, ErrNoActiveVersion) {
		t.Fatalf("Active on empty: got %v, want ErrNoActiveVersion", err)
	}
	if err := s.SetActive("b5046"); err != nil {
		t.Fatalf("SetActive: %v", err)
	}
	got, err := s.Active()
	if err != nil {
		t.Fatalf("Active: %v", err)
	}
	if got != "b5046" {
		t.Fatalf("Active = %q, want b5046", got)
	}
	if err := s.ClearActive(); err != nil {
		t.Fatalf("ClearActive: %v", err)
	}
	if _, err := s.Active(); !errors.Is(err, ErrNoActiveVersion) {
		t.Fatalf("Active after clear: got %v, want ErrNoActiveVersion", err)
	}
	// ClearActive on missing file is a no-op.
	if err := s.ClearActive(); err != nil {
		t.Fatalf("ClearActive on missing: %v", err)
	}
}

func TestStore_Active_TolerantOfTrailingNewline(t *testing.T) {
	s := newTestStore(t)
	if err := os.MkdirAll(s.Root(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.CurrentFile(), []byte("  b5046\n\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := s.Active()
	if err != nil {
		t.Fatal(err)
	}
	if got != "b5046" {
		t.Fatalf("Active = %q, want b5046", got)
	}
}

func TestStore_Remove(t *testing.T) {
	s := newTestStore(t)
	if err := os.MkdirAll(s.VersionDir("b5046"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := s.Remove("b5046"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if s.IsInstalled("b5046") {
		t.Fatal("expected version to be gone")
	}
	if err := s.Remove("b5046"); !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("Remove on missing: got %v, want ErrNotInstalled", err)
	}
}

func TestStore_PromoteStaging(t *testing.T) {
	s := newTestStore(t)
	staging := s.StagingDir("b5046")
	if err := os.MkdirAll(staging, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(staging, "marker")
	if err := os.WriteFile(marker, []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.PromoteStaging("b5046"); err != nil {
		t.Fatalf("PromoteStaging: %v", err)
	}
	if _, err := os.Stat(filepath.Join(s.VersionDir("b5046"), "marker")); err != nil {
		t.Fatalf("marker not in final dir: %v", err)
	}
	if _, err := os.Stat(staging); !os.IsNotExist(err) {
		t.Fatalf("staging dir should be gone, stat err = %v", err)
	}
}

func TestStore_RemoveStaging(t *testing.T) {
	s := newTestStore(t)
	staging := s.StagingDir("b5046")
	if err := os.MkdirAll(staging, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveStaging("b5046"); err != nil {
		t.Fatalf("RemoveStaging: %v", err)
	}
	if _, err := os.Stat(staging); !os.IsNotExist(err) {
		t.Fatal("staging dir should be gone")
	}
	// Idempotent.
	if err := s.RemoveStaging("b5046"); err != nil {
		t.Fatalf("RemoveStaging idempotent call: %v", err)
	}
}
