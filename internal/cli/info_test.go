package cli

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gregmundy/llamavm/internal/version"
)

// installVersionDir creates the on-disk layout `info` reads: the version dir
// itself, plus a `source/` subdir whose mere presence tells `info` git can
// be invoked there. Returns the version dir path.
func installVersionDir(t *testing.T, tag string) string {
	t.Helper()
	root := t.TempDir()
	versionDir := filepath.Join(root, ".llamavm", "versions", tag)
	if err := os.MkdirAll(filepath.Join(versionDir, "source"), 0o755); err != nil {
		t.Fatal(err)
	}
	return versionDir
}

func TestInfo_NoArgsResolvesFromCwd(t *testing.T) {
	versionDir := installVersionDir(t, "b9010")
	store := &fakeStore{
		installed:    []string{"b9010"},
		versionDirFn: func(_ string) string { return versionDir },
	}
	resolver := &fakeResolver{
		tag:        "b9010",
		source:     version.SourceCurrent,
		sourcePath: "/home/u/.llamavm/current",
	}
	cmd := &fakeCmdRunner{
		stdoutFn: func(name string, args []string, w io.Writer) {
			if name == "git" && len(args) >= 1 && args[0] == "rev-parse" {
				_, _ = w.Write([]byte("d05fe1d\n"))
			}
		},
	}
	deps := &Deps{
		Store:    store,
		Resolver: resolver,
		Git:      cmd,
		Getwd:    func() (string, error) { return "/home/u/proj", nil },
	}

	out, _, err := runRoot(t, deps, "info")
	if err != nil {
		t.Fatalf("info: %v", err)
	}
	for _, want := range []string{
		"Tag:    b9010",
		"Source: from /home/u/.llamavm/current",
		"Build:  d05fe1d (llama.cpp git SHA)",
		"Path:   " + versionDir,
		"Built:  ",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\nfull output:\n%s", want, out)
		}
	}
}

func TestInfo_PinSourceShowsPinPath(t *testing.T) {
	versionDir := installVersionDir(t, "b9009")
	store := &fakeStore{
		installed:    []string{"b9009"},
		versionDirFn: func(_ string) string { return versionDir },
	}
	resolver := &fakeResolver{
		tag:        "b9009",
		source:     version.SourcePin,
		sourcePath: "/home/u/proj/.llama-version",
	}
	deps := &Deps{
		Store:    store,
		Resolver: resolver,
		Git:      &fakeCmdRunner{},
		Getwd:    func() (string, error) { return "/home/u/proj", nil },
	}
	out, _, err := runRoot(t, deps, "info")
	if err != nil {
		t.Fatalf("info: %v", err)
	}
	if !strings.Contains(out, "Source: pinned at /home/u/proj/.llama-version") {
		t.Fatalf("expected pinned-at source line, got:\n%s", out)
	}
}

func TestInfo_ExplicitTagSkipsResolver(t *testing.T) {
	versionDir := installVersionDir(t, "b9010")
	store := &fakeStore{
		installed:    []string{"b9010"},
		versionDirFn: func(_ string) string { return versionDir },
	}
	resolver := &fakeResolver{} // should not be called
	deps := &Deps{
		Store:    store,
		Resolver: resolver,
		Git:      &fakeCmdRunner{},
		Getwd:    func() (string, error) { return "/home/u/proj", nil },
	}
	out, _, err := runRoot(t, deps, "info", "b9010")
	if err != nil {
		t.Fatalf("info b9010: %v", err)
	}
	if resolver.lastCwd != "" {
		t.Fatalf("Resolver should not be called when tag arg is supplied; lastCwd=%q", resolver.lastCwd)
	}
	if !strings.Contains(out, "Source: explicit (tag passed as argument)") {
		t.Fatalf("expected explicit-source line, got:\n%s", out)
	}
}

func TestInfo_TagNotInstalledIsUserError(t *testing.T) {
	store := &fakeStore{installed: []string{"b9010"}}
	deps := &Deps{
		Store:    store,
		Resolver: &fakeResolver{},
		Git:      &fakeCmdRunner{},
		Getwd:    func() (string, error) { return "/x", nil },
	}
	_, _, err := runRoot(t, deps, "info", "b9999")
	if err == nil {
		t.Fatal("expected error when tag is not installed")
	}
	if !errors.Is(err, ErrUserError) {
		t.Fatalf("err = %v, want chained ErrUserError", err)
	}
	if !strings.Contains(err.Error(), "llamavm install b9999") {
		t.Fatalf("err = %v, want it to suggest 'llamavm install b9999'", err)
	}
}

func TestInfo_NoActiveVersionIsUserError(t *testing.T) {
	store := &fakeStore{}
	deps := &Deps{
		Store:    store,
		Resolver: &fakeResolver{err: version.ErrNoActiveVersion},
		Git:      &fakeCmdRunner{},
		Getwd:    func() (string, error) { return "/x", nil },
	}
	_, _, err := runRoot(t, deps, "info")
	if err == nil {
		t.Fatal("expected error when no active version")
	}
	if !errors.Is(err, ErrUserError) {
		t.Fatalf("err = %v, want chained ErrUserError", err)
	}
}

func TestInfo_MissingSourceDirShowsUnknownSHA(t *testing.T) {
	// Create a version dir but NOT the source/ subdir → readBuildSHA hits
	// stat-error path and returns "(unknown)".
	root := t.TempDir()
	versionDir := filepath.Join(root, ".llamavm", "versions", "b9010")
	if err := os.MkdirAll(versionDir, 0o755); err != nil {
		t.Fatal(err)
	}
	store := &fakeStore{
		installed:    []string{"b9010"},
		versionDirFn: func(_ string) string { return versionDir },
	}
	cmd := &fakeCmdRunner{}
	deps := &Deps{
		Store:    store,
		Resolver: &fakeResolver{},
		Git:      cmd,
		Getwd:    func() (string, error) { return "/x", nil },
	}
	out, _, err := runRoot(t, deps, "info", "b9010")
	if err != nil {
		t.Fatalf("info: %v", err)
	}
	if !strings.Contains(out, "Build:  (unknown)") {
		t.Fatalf("expected '(unknown)' SHA when source dir missing; got:\n%s", out)
	}
	// Verify git was NOT invoked when there's no source dir to run it in.
	for _, c := range cmd.calls {
		if c.Name == "git" {
			t.Fatalf("git should not be invoked when source dir is missing; calls=%+v", cmd.calls)
		}
	}
}

// silence unused-import warning when tests are filtered.
var _ = context.Background
