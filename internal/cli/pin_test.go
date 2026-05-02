package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// pinCwd is a small helper: returns a Getwd that points at a fresh temp dir,
// plus the dir itself.
func pinCwd(t *testing.T) (string, func() (string, error)) {
	t.Helper()
	dir := t.TempDir()
	return dir, func() (string, error) { return dir, nil }
}

func TestPin_RequiresVersionArg(t *testing.T) {
	_, getwd := pinCwd(t)
	deps := &Deps{Store: &fakeStore{}, Getwd: getwd}
	if _, _, err := runRoot(t, deps, "pin"); err == nil {
		t.Fatal("expected error when version arg missing")
	}
}

func TestPin_InvalidTagFormatIsUserError(t *testing.T) {
	cases := []string{
		"",
		"   ",
		"foo bar",
		"foo\nbar",
		"../escape",
		"a/b",
	}
	for _, bad := range cases {
		t.Run(bad, func(t *testing.T) {
			cwd, getwd := pinCwd(t)
			deps := &Deps{Store: &fakeStore{installed: []string{bad}}, Getwd: getwd}
			_, _, err := runRoot(t, deps, "pin", bad)
			if err == nil {
				t.Fatalf("expected error for tag %q", bad)
			}
			if !errors.Is(err, ErrUserError) {
				t.Fatalf("err = %v, want chained ErrUserError", err)
			}
			if _, statErr := os.Stat(filepath.Join(cwd, ".llama-version")); statErr == nil {
				t.Fatalf("invalid tag %q wrote .llama-version anyway", bad)
			}
		})
	}
}

func TestPin_HappyPathWritesFile(t *testing.T) {
	cwd, getwd := pinCwd(t)
	store := &fakeStore{installed: []string{"b5046"}}
	deps := &Deps{Store: store, Getwd: getwd}

	out, errOut, err := runRoot(t, deps, "pin", "b5046")
	if err != nil {
		t.Fatalf("pin: %v", err)
	}
	body, readErr := os.ReadFile(filepath.Join(cwd, ".llama-version"))
	if readErr != nil {
		t.Fatalf("read .llama-version: %v", readErr)
	}
	if string(body) != "b5046\n" {
		t.Fatalf(".llama-version body = %q, want \"b5046\\n\"", string(body))
	}
	if !strings.Contains(out, "b5046") {
		t.Fatalf("stdout = %q, want it to mention b5046", out)
	}
	if errOut != "" {
		t.Fatalf("stderr = %q, want empty (no warning when installed)", errOut)
	}
}

func TestPin_OverwritesExistingFile(t *testing.T) {
	cwd, getwd := pinCwd(t)
	if err := os.WriteFile(filepath.Join(cwd, ".llama-version"), []byte("b9999\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := &fakeStore{installed: []string{"b5046"}}
	deps := &Deps{Store: store, Getwd: getwd}

	if _, _, err := runRoot(t, deps, "pin", "b5046"); err != nil {
		t.Fatalf("pin: %v", err)
	}
	body, _ := os.ReadFile(filepath.Join(cwd, ".llama-version"))
	if string(body) != "b5046\n" {
		t.Fatalf(".llama-version body = %q, want \"b5046\\n\"", string(body))
	}
}

func TestPin_NotInstalledWarnsAndStillWrites(t *testing.T) {
	cwd, getwd := pinCwd(t)
	store := &fakeStore{installed: []string{"b5489"}} // b5046 is NOT installed
	deps := &Deps{Store: store, Getwd: getwd}

	_, errOut, err := runRoot(t, deps, "pin", "b5046")
	if err != nil {
		t.Fatalf("pin: %v", err)
	}
	body, readErr := os.ReadFile(filepath.Join(cwd, ".llama-version"))
	if readErr != nil {
		t.Fatalf("expected file to be written even when tag is not installed: %v", readErr)
	}
	if string(body) != "b5046\n" {
		t.Fatalf(".llama-version body = %q, want \"b5046\\n\"", string(body))
	}
	if !strings.Contains(strings.ToLower(errOut), "warning") {
		t.Fatalf("stderr = %q, want it to contain a warning", errOut)
	}
	if !strings.Contains(errOut, "b5046") {
		t.Fatalf("stderr = %q, want it to mention b5046", errOut)
	}
	if !strings.Contains(errOut, "llamavm install") {
		t.Fatalf("stderr = %q, want remediation to mention 'llamavm install'", errOut)
	}
}

func TestPin_GetwdErrorPropagates(t *testing.T) {
	deps := &Deps{
		Store: &fakeStore{installed: []string{"b5046"}},
		Getwd: func() (string, error) { return "", errors.New("getwd failed") },
	}
	if _, _, err := runRoot(t, deps, "pin", "b5046"); err == nil {
		t.Fatal("expected error when Getwd fails")
	}
}

func TestPin_WriteFailureIsErrored(t *testing.T) {
	// Point Getwd at a non-writable target (a regular file, not a directory)
	// so the os.WriteFile under it will fail.
	parent := t.TempDir()
	notADir := filepath.Join(parent, "block")
	if err := os.WriteFile(notADir, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	deps := &Deps{
		Store: &fakeStore{installed: []string{"b5046"}},
		Getwd: func() (string, error) { return notADir, nil },
	}
	if _, _, err := runRoot(t, deps, "pin", "b5046"); err == nil {
		t.Fatal("expected error when target dir is not a directory")
	}
}
