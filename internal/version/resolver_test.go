package version

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// writeFile is a small helper: write body (with trailing newline if nl) at path.
func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestResolver_NoActiveVersion(t *testing.T) {
	home := t.TempDir()
	r := NewResolver(New(home), home)
	if _, err := r.Resolve(""); !errors.Is(err, ErrNoActiveVersion) {
		t.Fatalf("Resolve(\"\") on empty: got %v, want ErrNoActiveVersion", err)
	}
}

func TestResolver_ReadsCurrentFileWhenNoCwd(t *testing.T) {
	home := t.TempDir()
	s := New(home)
	if err := s.SetActive("b5046"); err != nil {
		t.Fatal(err)
	}
	r := NewResolver(s, home)
	got, err := r.Resolve("")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != "b5046" {
		t.Fatalf("Resolve = %q, want b5046", got)
	}
}

func TestResolver_FindsLlamaVersionInCwd(t *testing.T) {
	home := t.TempDir()
	cwd := filepath.Join(home, "project")
	writeFile(t, filepath.Join(cwd, ".llama-version"), "b5489\n")

	r := NewResolver(New(home), home)
	got, err := r.Resolve(cwd)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != "b5489" {
		t.Fatalf("Resolve = %q, want b5489", got)
	}
}

func TestResolver_FindsLlamaVersionInAncestor(t *testing.T) {
	home := t.TempDir()
	parent := filepath.Join(home, "workspace")
	cwd := filepath.Join(parent, "deep", "deeper")
	writeFile(t, filepath.Join(parent, ".llama-version"), "b5400\n")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}

	r := NewResolver(New(home), home)
	got, err := r.Resolve(cwd)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != "b5400" {
		t.Fatalf("Resolve = %q, want b5400", got)
	}
}

func TestResolver_PrefersCwdOverAncestor(t *testing.T) {
	home := t.TempDir()
	parent := filepath.Join(home, "workspace")
	cwd := filepath.Join(parent, "child")
	writeFile(t, filepath.Join(parent, ".llama-version"), "b5400\n")
	writeFile(t, filepath.Join(cwd, ".llama-version"), "b5489\n")

	r := NewResolver(New(home), home)
	got, err := r.Resolve(cwd)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != "b5489" {
		t.Fatalf("Resolve = %q, want b5489 (cwd wins over ancestor)", got)
	}
}

func TestResolver_PrefersLlamaVersionOverCurrentFile(t *testing.T) {
	home := t.TempDir()
	cwd := filepath.Join(home, "project")
	writeFile(t, filepath.Join(cwd, ".llama-version"), "b5489\n")
	s := New(home)
	if err := s.SetActive("b5046"); err != nil {
		t.Fatal(err)
	}

	r := NewResolver(s, home)
	got, err := r.Resolve(cwd)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != "b5489" {
		t.Fatalf("Resolve = %q, want b5489 (.llama-version wins over current)", got)
	}
}

func TestResolver_StopsAtHome(t *testing.T) {
	// .llama-version is one level above home — must NOT be picked up.
	above := t.TempDir()
	home := filepath.Join(above, "home")
	cwd := filepath.Join(home, "project")
	writeFile(t, filepath.Join(above, ".llama-version"), "b9999\n")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}

	s := New(home)
	if err := s.SetActive("b5046"); err != nil {
		t.Fatal(err)
	}
	r := NewResolver(s, home)
	got, err := r.Resolve(cwd)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != "b5046" {
		t.Fatalf("Resolve = %q, want b5046 (walk should not cross home boundary)", got)
	}
}

func TestResolver_ChecksHomeItself(t *testing.T) {
	// .llama-version directly in home is in scope — the walk includes home.
	home := t.TempDir()
	writeFile(t, filepath.Join(home, ".llama-version"), "b5489\n")

	r := NewResolver(New(home), home)
	got, err := r.Resolve(home)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != "b5489" {
		t.Fatalf("Resolve = %q, want b5489", got)
	}
}

func TestResolver_TrimsWhitespace(t *testing.T) {
	home := t.TempDir()
	cwd := filepath.Join(home, "project")
	writeFile(t, filepath.Join(cwd, ".llama-version"), "  b5489\n\n")

	r := NewResolver(New(home), home)
	got, err := r.Resolve(cwd)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != "b5489" {
		t.Fatalf("Resolve = %q, want b5489 (trimmed)", got)
	}
}

func TestResolver_EmptyFileFallsThrough(t *testing.T) {
	// An empty .llama-version is treated as no pin: keep walking, then fall
	// back to ~/.llamavm/current.
	home := t.TempDir()
	cwd := filepath.Join(home, "project")
	writeFile(t, filepath.Join(cwd, ".llama-version"), "   \n")
	s := New(home)
	if err := s.SetActive("b5046"); err != nil {
		t.Fatal(err)
	}

	r := NewResolver(s, home)
	got, err := r.Resolve(cwd)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != "b5046" {
		t.Fatalf("Resolve = %q, want b5046 (empty pin file should fall through)", got)
	}
}

func TestResolver_FallsBackToCurrentFileWhenNoPin(t *testing.T) {
	home := t.TempDir()
	cwd := filepath.Join(home, "deep", "deeper")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	s := New(home)
	if err := s.SetActive("b5046"); err != nil {
		t.Fatal(err)
	}

	r := NewResolver(s, home)
	got, err := r.Resolve(cwd)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != "b5046" {
		t.Fatalf("Resolve = %q, want b5046", got)
	}
}

func TestResolver_NoPinNoCurrentReturnsErr(t *testing.T) {
	home := t.TempDir()
	cwd := filepath.Join(home, "project")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	r := NewResolver(New(home), home)
	if _, err := r.Resolve(cwd); !errors.Is(err, ErrNoActiveVersion) {
		t.Fatalf("Resolve: got %v, want ErrNoActiveVersion", err)
	}
}
