package shim

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type recordedExec struct {
	path string
	argv []string
	envv []string
}

func newFakeExec() (*recordedExec, func(string, []string, []string) error) {
	rec := &recordedExec{}
	return rec, func(p string, argv, envv []string) error {
		rec.path = p
		rec.argv = append([]string(nil), argv...)
		rec.envv = append([]string(nil), envv...)
		return nil
	}
}

// makeShimsTree creates a versions tree with one tag and the requested binary names
// under <root>/versions/<tag>/bin. Returns the versions dir.
func makeShimsTree(t *testing.T, tag string, names ...string) string {
	t.Helper()
	root := t.TempDir()
	binDir := filepath.Join(root, "versions", tag, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(binDir, n), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return filepath.Join(root, "versions")
}

func TestRun_HappyPath(t *testing.T) {
	versionsDir := makeShimsTree(t, "b5046", "llama-cli")
	rec, fakeExec := newFakeExec()
	shimPath := filepath.Join(t.TempDir(), "shims", "llama-cli")

	code := Run(Options{
		Argv:        []string{shimPath, "--version"},
		Resolver:    func() (string, error) { return "b5046", nil },
		VersionsDir: versionsDir,
		Stderr:      io.Discard,
		Env:         []string{"FOO=bar"},
		ExecFn:      fakeExec,
	})
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	want := filepath.Join(versionsDir, "b5046", "bin", "llama-cli")
	if rec.path != want {
		t.Fatalf("exec path = %q, want %q", rec.path, want)
	}
	if len(rec.argv) != 2 || rec.argv[0] != shimPath || rec.argv[1] != "--version" {
		t.Fatalf("exec argv = %v, want [%s --version]", rec.argv, shimPath)
	}
	if len(rec.envv) != 1 || rec.envv[0] != "FOO=bar" {
		t.Fatalf("exec env = %v, want [FOO=bar]", rec.envv)
	}
}

func TestRun_UsesArgv0Basename(t *testing.T) {
	versionsDir := makeShimsTree(t, "b5046", "llama-server")
	rec, fakeExec := newFakeExec()
	shimPath := filepath.Join(t.TempDir(), "shims", "llama-server")

	code := Run(Options{
		Argv:        []string{shimPath, "-m", "model.gguf"},
		Resolver:    func() (string, error) { return "b5046", nil },
		VersionsDir: versionsDir,
		Stderr:      io.Discard,
		Env:         nil,
		ExecFn:      fakeExec,
	})
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if filepath.Base(rec.path) != "llama-server" {
		t.Fatalf("target binary = %q, want llama-server", rec.path)
	}
}

func TestRun_NoActiveVersion_Exit127(t *testing.T) {
	var stderr bytes.Buffer
	code := Run(Options{
		Argv:        []string{"/shims/llama-cli"},
		Resolver:    func() (string, error) { return "", errors.New("no active version") },
		VersionsDir: t.TempDir(),
		Stderr:      &stderr,
		ExecFn:      func(string, []string, []string) error { t.Fatal("exec should not run"); return nil },
	})
	if code != 127 {
		t.Fatalf("exit code = %d, want 127", code)
	}
	if !strings.Contains(stderr.String(), "no active version") {
		t.Fatalf("stderr = %q, want it to mention 'no active version'", stderr.String())
	}
}

func TestRun_BinaryMissing_Exit127(t *testing.T) {
	versionsDir := makeShimsTree(t, "b5046")
	var stderr bytes.Buffer
	code := Run(Options{
		Argv:        []string{"/shims/llama-cli"},
		Resolver:    func() (string, error) { return "b5046", nil },
		VersionsDir: versionsDir,
		Stderr:      &stderr,
		ExecFn:      func(string, []string, []string) error { t.Fatal("exec should not run"); return nil },
	})
	if code != 127 {
		t.Fatalf("exit code = %d, want 127", code)
	}
	if !strings.Contains(stderr.String(), "llama-cli") {
		t.Fatalf("stderr = %q, want it to mention 'llama-cli'", stderr.String())
	}
}

func TestRun_ExecFails_Exit127(t *testing.T) {
	versionsDir := makeShimsTree(t, "b5046", "llama-cli")
	var stderr bytes.Buffer
	code := Run(Options{
		Argv:        []string{"/shims/llama-cli"},
		Resolver:    func() (string, error) { return "b5046", nil },
		VersionsDir: versionsDir,
		Stderr:      &stderr,
		ExecFn:      func(string, []string, []string) error { return errors.New("exec broken") },
	})
	if code != 127 {
		t.Fatalf("exit code = %d, want 127", code)
	}
	if !strings.Contains(stderr.String(), "exec broken") {
		t.Fatalf("stderr = %q, want it to mention 'exec broken'", stderr.String())
	}
}
