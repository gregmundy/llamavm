package shim

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// writeFakeShim writes a placeholder "binary" file and returns its path.
func writeFakeShim(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "llamavm-shim")
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestInstaller_WritesAllShims(t *testing.T) {
	src := writeFakeShim(t, "fake-shim-v1")
	shimsDir := filepath.Join(t.TempDir(), "shims")
	inst := &Installer{Source: func() (string, error) { return src, nil }}

	if err := inst.EnsureInstalled(shimsDir); err != nil {
		t.Fatalf("EnsureInstalled: %v", err)
	}
	for _, name := range Names {
		got, err := os.ReadFile(filepath.Join(shimsDir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if string(got) != "fake-shim-v1" {
			t.Fatalf("%s body = %q, want fake-shim-v1", name, string(got))
		}
		fi, err := os.Stat(filepath.Join(shimsDir, name))
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm()&0o100 == 0 {
			t.Fatalf("%s is not executable: mode=%v", name, fi.Mode())
		}
	}
}

func TestInstaller_IsIdempotent(t *testing.T) {
	src := writeFakeShim(t, "v1")
	shimsDir := filepath.Join(t.TempDir(), "shims")
	calls := 0
	inst := &Installer{Source: func() (string, error) {
		calls++
		return src, nil
	}}
	if err := inst.EnsureInstalled(shimsDir); err != nil {
		t.Fatal(err)
	}
	mutated := filepath.Join(shimsDir, "llama-cli")
	if err := os.WriteFile(mutated, []byte("user-modified"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := inst.EnsureInstalled(shimsDir); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(mutated)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "user-modified" {
		t.Fatalf("idempotent install overwrote existing shim: body = %q", string(got))
	}
	if calls == 0 {
		t.Fatal("Source was never called")
	}
}

func TestInstaller_SourceErrorPropagates(t *testing.T) {
	inst := &Installer{Source: func() (string, error) { return "", errors.New("no shim source") }}
	err := inst.EnsureInstalled(filepath.Join(t.TempDir(), "shims"))
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestInstaller_CreatesShimsDir(t *testing.T) {
	src := writeFakeShim(t, "v1")
	root := t.TempDir()
	shimsDir := filepath.Join(root, "deep", "shims")
	inst := &Installer{Source: func() (string, error) { return src, nil }}
	if err := inst.EnsureInstalled(shimsDir); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(shimsDir)
	if err != nil || !fi.IsDir() {
		t.Fatalf("expected shimsDir to be created, stat err=%v isDir=%v", err, fi != nil && fi.IsDir())
	}
}

func TestInstaller_StatExistingIsNotAnError(t *testing.T) {
	src := writeFakeShim(t, "v1")
	shimsDir := filepath.Join(t.TempDir(), "shims")
	if err := os.MkdirAll(shimsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(shimsDir, "llama-cli"), []byte("pre-existing"), 0o755); err != nil {
		t.Fatal(err)
	}
	inst := &Installer{Source: func() (string, error) { return src, nil }}
	if err := inst.EnsureInstalled(shimsDir); err != nil {
		if errors.Is(err, fs.ErrExist) {
			t.Fatalf("EnsureInstalled returned fs.ErrExist on pre-existing shim: %v", err)
		}
		t.Fatalf("EnsureInstalled: %v", err)
	}
}
