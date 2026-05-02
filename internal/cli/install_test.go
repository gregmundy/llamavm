package cli

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gh "github.com/gregmundy/llamavm/internal/github"
)

// fakeGitHub implements GitHubClient.
type fakeGitHub struct {
	latest      string
	latestErr   error
	tagErr      error
	calledTag   string
	latestCalls int
}

func (g *fakeGitHub) Latest(ctx context.Context) (string, error) {
	g.latestCalls++
	return g.latest, g.latestErr
}
func (g *fakeGitHub) TagExists(ctx context.Context, tag string) error {
	g.calledTag = tag
	return g.tagErr
}

// fakeBuilder implements Builder.
type fakeBuilder struct {
	srcDir string
	err    error
	logOut string
}

func (b *fakeBuilder) Build(ctx context.Context, srcDir string, log io.Writer) error {
	b.srcDir = srcDir
	if b.logOut != "" {
		log.Write([]byte(b.logOut))
	}
	return b.err
}

// fakeRunner implements CommandRunner. Used for git clone.
type fakeCmdRunner struct {
	calls   []recordedCall
	cloneFn func(args []string, dir string) error
}

type recordedCall struct {
	Name string
	Args []string
	Dir  string
}

func (r *fakeCmdRunner) Run(ctx context.Context, name string, args []string, dir string, stdout, stderr io.Writer) error {
	r.calls = append(r.calls, recordedCall{Name: name, Args: append([]string(nil), args...), Dir: dir})
	if r.cloneFn != nil {
		return r.cloneFn(args, dir)
	}
	return nil
}

// fakePlatform implements Platform.
type fakePlatform struct{ apple bool }

func (p fakePlatform) IsAppleSilicon() bool { return p.apple }

// realPathStore is a Store that uses real temp dirs so install can create
// staging directories on disk.
type realPathStore struct {
	*fakeStore
	root string
}

func newRealPathStore(t *testing.T, installed ...string) *realPathStore {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "versions"), 0o755); err != nil {
		t.Fatal(err)
	}
	rps := &realPathStore{
		fakeStore: &fakeStore{installed: append([]string(nil), installed...)},
		root:      root,
	}
	rps.fakeStore.versionDirFn = func(tag string) string {
		return filepath.Join(root, "versions", tag)
	}
	rps.fakeStore.stagingDirFn = func(tag string) string {
		return filepath.Join(root, "versions", ".staging-"+tag)
	}
	rps.fakeStore.logsDir = filepath.Join(root, "logs")
	return rps
}

// PromoteStaging on realPathStore actually renames so install can verify outcome.
func (s *realPathStore) PromoteStaging(tag string) error {
	from := filepath.Join(s.root, "versions", ".staging-"+tag)
	to := filepath.Join(s.root, "versions", tag)
	if err := os.Rename(from, to); err != nil {
		return err
	}
	s.installed = append(s.installed, tag)
	return nil
}

func (s *realPathStore) RemoveStaging(tag string) error {
	return os.RemoveAll(filepath.Join(s.root, "versions", ".staging-"+tag))
}

func newInstallDeps(t *testing.T, store Store) (*Deps, *fakeGitHub, *fakeBuilder, *fakeCmdRunner) {
	t.Helper()
	g := &fakeGitHub{latest: "b5489"}
	b := &fakeBuilder{}
	r := &fakeCmdRunner{cloneFn: func(args []string, dir string) error {
		// Simulate git clone: create the destination directory with a build/bin/ tree.
		// Last arg of git clone is destination.
		dst := args[len(args)-1]
		bin := filepath.Join(dst, "build", "bin")
		if err := os.MkdirAll(bin, 0o755); err != nil {
			return err
		}
		for _, name := range []string{"llama-cli", "llama-server", "llama-quantize"} {
			if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"), 0o755); err != nil {
				return err
			}
		}
		return nil
	}}
	deps := &Deps{
		Store:    store,
		GitHub:   g,
		Builder:  b,
		Git:      r,
		Platform: fakePlatform{apple: true},
		Now:      func() time.Time { return time.Date(2026, 5, 2, 14, 30, 30, 0, time.UTC) },
	}
	return deps, g, b, r
}

func TestInstall_RequiresVersion(t *testing.T) {
	deps, _, _, _ := newInstallDeps(t, &fakeStore{})
	if _, _, err := runRoot(t, deps, "install"); err == nil {
		t.Fatal("expected error when version arg missing")
	}
}

func TestInstall_RefusesNonAppleSilicon(t *testing.T) {
	store := newRealPathStore(t)
	deps, _, _, _ := newInstallDeps(t, store)
	deps.Platform = fakePlatform{apple: false}
	_, _, err := runRoot(t, deps, "install", "b5046")
	if err == nil || !strings.Contains(err.Error(), "Apple Silicon") {
		t.Fatalf("err = %v, want one mentioning Apple Silicon", err)
	}
}

func TestInstall_AlreadyInstalledIsIdempotent(t *testing.T) {
	store := newRealPathStore(t, "b5046")
	deps, g, b, r := newInstallDeps(t, store)
	out, _, err := runRoot(t, deps, "install", "b5046")
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if !strings.Contains(out, "already installed") {
		t.Fatalf("stdout = %q, want 'already installed'", out)
	}
	if g.calledTag != "" || len(r.calls) != 0 || b.srcDir != "" {
		t.Fatalf("expected no network/clone/build; gh=%q, calls=%d, build=%q",
			g.calledTag, len(r.calls), b.srcDir)
	}
}

func TestInstall_LatestResolvesViaGitHub(t *testing.T) {
	store := newRealPathStore(t)
	deps, g, _, r := newInstallDeps(t, store)
	g.latest = "b5489"
	if _, _, err := runRoot(t, deps, "install", "latest"); err != nil {
		t.Fatalf("install latest: %v", err)
	}
	if g.latestCalls != 1 {
		t.Fatalf("expected 1 Latest call, got %d", g.latestCalls)
	}
	// Final installed dir should be the resolved tag.
	if !store.IsInstalled("b5489") {
		t.Fatal("expected b5489 to be installed")
	}
	// Git clone should have been called with --branch b5489.
	if len(r.calls) == 0 || !contains(r.calls[0].Args, "b5489") {
		t.Fatalf("expected git clone to use resolved tag; calls=%+v", r.calls)
	}
}

func TestInstall_TagNotFound(t *testing.T) {
	store := newRealPathStore(t)
	deps, g, _, _ := newInstallDeps(t, store)
	g.tagErr = gh.ErrTagNotFound
	_, _, err := runRoot(t, deps, "install", "b9999")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("err = %v, want one mentioning 'not found'", err)
	}
}

func TestInstall_HappyPath(t *testing.T) {
	store := newRealPathStore(t)
	deps, _, b, r := newInstallDeps(t, store)
	out, _, err := runRoot(t, deps, "install", "b5046")
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if !store.IsInstalled("b5046") {
		t.Fatal("expected b5046 installed after happy path")
	}
	// Active should be set on first install.
	active, _ := store.Active()
	if active != "b5046" {
		t.Fatalf("active = %q, want b5046", active)
	}
	// Clone target should be staging/source.
	if len(r.calls) == 0 {
		t.Fatal("expected git clone")
	}
	cloneDst := r.calls[0].Args[len(r.calls[0].Args)-1]
	if !strings.HasSuffix(cloneDst, filepath.Join("versions", ".staging-b5046", "source")) {
		t.Fatalf("clone dst = %q, want it to end at versions/.staging-b5046/source", cloneDst)
	}
	// Builder should have run inside source dir.
	if !strings.HasSuffix(b.srcDir, "source") {
		t.Fatalf("builder srcDir = %q", b.srcDir)
	}
	// Bin symlinks must exist in final dir.
	finalBin := filepath.Join(store.root, "versions", "b5046", "bin")
	for _, name := range []string{"llama-cli", "llama-server", "llama-quantize"} {
		fi, err := os.Lstat(filepath.Join(finalBin, name))
		if err != nil {
			t.Fatalf("expected symlink %s: %v", name, err)
		}
		if fi.Mode()&os.ModeSymlink == 0 {
			t.Errorf("%s is not a symlink (mode=%v)", name, fi.Mode())
		}
	}
	if !strings.Contains(out, "Installed b5046") {
		t.Fatalf("stdout = %q, want 'Installed b5046'", out)
	}
}

func TestInstall_KeepsActiveOnSecondInstall(t *testing.T) {
	store := newRealPathStore(t)
	if err := store.SetActive("b5046"); err != nil {
		t.Fatal(err)
	}
	store.installed = append(store.installed, "b5046")
	deps, _, _, _ := newInstallDeps(t, store)
	if _, _, err := runRoot(t, deps, "install", "b5489"); err != nil {
		t.Fatalf("install: %v", err)
	}
	active, _ := store.Active()
	if active != "b5046" {
		t.Fatalf("active = %q, want b5046 (unchanged)", active)
	}
}

func TestInstall_BuildFailureIsAtomic(t *testing.T) {
	store := newRealPathStore(t)
	deps, _, b, _ := newInstallDeps(t, store)
	b.err = errors.New("cmake exited 1")
	b.logOut = "metal not found\n"

	_, _, err := runRoot(t, deps, "install", "b5046")
	if err == nil {
		t.Fatal("expected error on build failure")
	}
	if store.IsInstalled("b5046") {
		t.Fatal("failed install should not appear as installed")
	}
	stagingPath := filepath.Join(store.root, "versions", ".staging-b5046")
	if _, err := os.Stat(stagingPath); !os.IsNotExist(err) {
		t.Fatalf("staging should be removed on failure, stat err = %v", err)
	}
	// Build log should be preserved under logs/.
	logPath := filepath.Join(store.root, "logs", "b5046-20260502T143030.log")
	body, readErr := os.ReadFile(logPath)
	if readErr != nil {
		t.Fatalf("expected build log at %s: %v", logPath, readErr)
	}
	if !strings.Contains(string(body), "metal not found") {
		t.Fatalf("log body = %q, want it to contain stderr", string(body))
	}
	if !strings.Contains(err.Error(), logPath) {
		t.Fatalf("err = %v, want it to mention log path %s", err, logPath)
	}
}

func TestInstall_GitFailureIsAtomic(t *testing.T) {
	store := newRealPathStore(t)
	deps, _, _, r := newInstallDeps(t, store)
	r.cloneFn = func(args []string, dir string) error { return errors.New("clone refused") }

	_, _, err := runRoot(t, deps, "install", "b5046")
	if err == nil {
		t.Fatal("expected error on clone failure")
	}
	if store.IsInstalled("b5046") {
		t.Fatal("failed install should not appear as installed")
	}
	stagingPath := filepath.Join(store.root, "versions", ".staging-b5046")
	if _, err := os.Stat(stagingPath); !os.IsNotExist(err) {
		t.Fatalf("staging should be removed, stat err = %v", err)
	}
}

func TestInstall_TagNotFound_IsUserError(t *testing.T) {
	store := newRealPathStore(t)
	deps, g, _, _ := newInstallDeps(t, store)
	g.tagErr = gh.ErrTagNotFound
	_, _, err := runRoot(t, deps, "install", "b9999")
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrUserError) {
		t.Fatalf("err = %v, want it to chain to ErrUserError", err)
	}
}

func TestInstall_InvalidTagShape(t *testing.T) {
	store := newRealPathStore(t)
	deps, _, _, _ := newInstallDeps(t, store)
	_, _, err := runRoot(t, deps, "install", "../etc/passwd")
	if err == nil {
		t.Fatal("expected error on invalid tag")
	}
	if !errors.Is(err, ErrUserError) {
		t.Fatalf("err = %v, want it to chain to ErrUserError", err)
	}
}

func contains(slice []string, want string) bool {
	for _, s := range slice {
		if s == want {
			return true
		}
	}
	return false
}
