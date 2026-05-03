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

// testInstalledBinaries is the set of llama-* executables the simulated
// "git clone" fixture writes into build/bin. It's deliberately wider than
// the historical hardcoded list (adds llama-bench) so install tests
// exercise the dynamic discovery path against multiple matches.
var testInstalledBinaries = []string{"llama-cli", "llama-server", "llama-quantize", "llama-bench"}

func TestDiscoverLlamaBinaries_FiltersToExecutableLlamaPrefix(t *testing.T) {
	dir := t.TempDir()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	// Matching: llama-* + executable
	must(os.WriteFile(filepath.Join(dir, "llama-cli"), []byte("x"), 0o755))
	must(os.WriteFile(filepath.Join(dir, "llama-embedding"), []byte("x"), 0o755))
	must(os.WriteFile(filepath.Join(dir, "llama-tokenize"), []byte("x"), 0o755))
	// Skipped: not in llama-* namespace
	must(os.WriteFile(filepath.Join(dir, "export-graph-ops"), []byte("x"), 0o755))
	must(os.WriteFile(filepath.Join(dir, "ggml-bench"), []byte("x"), 0o755))
	// Skipped: llama-* but not executable
	must(os.WriteFile(filepath.Join(dir, "llama-readme.md"), []byte("docs"), 0o644))
	// Skipped: directory
	must(os.MkdirAll(filepath.Join(dir, "llama-subdir"), 0o755))
	// Skipped: symlink (cmake-style versioned dylib chain)
	must(os.WriteFile(filepath.Join(dir, "llama-real"), []byte("x"), 0o755))
	must(os.Symlink("llama-real", filepath.Join(dir, "llama-link")))

	got, err := discoverLlamaBinaries(dir)
	if err != nil {
		t.Fatalf("discoverLlamaBinaries: %v", err)
	}
	want := []string{"llama-cli", "llama-embedding", "llama-real", "llama-tokenize"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i, n := range want {
		if got[i] != n {
			t.Fatalf("got[%d] = %q, want %q (full got=%v)", i, got[i], n, got)
		}
	}
}

func TestDiscoverLlamaBinaries_MissingDirIsError(t *testing.T) {
	if _, err := discoverLlamaBinaries("/no/such/path"); err == nil {
		t.Fatal("expected error when build/bin is missing")
	}
}

func TestDiscoverLlamaBinaries_NoMatchesIsError(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ggml-bench"), []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := discoverLlamaBinaries(dir)
	if err == nil {
		t.Fatal("expected error when no llama-* executables found")
	}
	if !strings.Contains(err.Error(), "no llama-") {
		t.Fatalf("err = %v, want it to mention 'no llama-'", err)
	}
}

func TestSummarizeShims_ShortListNoTruncation(t *testing.T) {
	got := summarizeShims([]string{"a", "b", "c"})
	want := "3 shims: a, b, c"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestSummarizeShims_LongListTruncatesAndCounts(t *testing.T) {
	got := summarizeShims([]string{"a", "b", "c", "d", "e", "f", "g", "h"})
	want := "8 shims: a, b, c, d, e (... 3 more)"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestInstall_HappyPathPrintsShimSummary(t *testing.T) {
	store := newRealPathStore(t)
	deps, _, _, _, _ := newInstallDeps(t, store)
	out, _, err := runRoot(t, deps, "install", "b5046")
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	// All four llama-* test binaries should appear in the shim summary.
	if !strings.Contains(out, "with 4 shims:") {
		t.Fatalf("expected install line to summarize 4 shims; out=%s", out)
	}
	for _, name := range testInstalledBinaries {
		if !strings.Contains(out, name) {
			t.Fatalf("install line missing shim name %q; out=%s", name, out)
		}
	}
}

func TestInstall_PassesDiscoveredNamesToShimInstaller(t *testing.T) {
	store := newRealPathStore(t)
	deps, _, _, _, si := newInstallDeps(t, store)
	if _, _, err := runRoot(t, deps, "install", "b5046"); err != nil {
		t.Fatalf("install: %v", err)
	}
	if len(si.gotNames) != 1 {
		t.Fatalf("ShimInstaller calls = %d, want 1", len(si.gotNames))
	}
	got := si.gotNames[0]
	// Must include all four discovered binaries; must NOT include the
	// non-llama helpers we wrote alongside in the fixture.
	wantSet := map[string]bool{
		"llama-cli": true, "llama-server": true,
		"llama-quantize": true, "llama-bench": true,
	}
	for _, name := range got {
		if !wantSet[name] {
			t.Fatalf("ShimInstaller received unexpected name %q (got=%v)", name, got)
		}
		delete(wantSet, name)
	}
	if len(wantSet) != 0 {
		t.Fatalf("ShimInstaller missing names: %v (got=%v)", wantSet, got)
	}
}

// fakeGitHub implements GitHubClient.
type fakeGitHub struct {
	latest      string
	latestErr   error
	tagErr      error
	calledTag   string
	latestCalls int

	listTags     []string
	listErr      error
	listLastArgs struct {
		limit int
		all   bool
	}
}

func (g *fakeGitHub) Latest(ctx context.Context) (string, error) {
	g.latestCalls++
	return g.latest, g.latestErr
}
func (g *fakeGitHub) TagExists(ctx context.Context, tag string) error {
	g.calledTag = tag
	return g.tagErr
}
func (g *fakeGitHub) ListReleases(ctx context.Context, limit int, all bool) ([]string, error) {
	g.listLastArgs.limit = limit
	g.listLastArgs.all = all
	if g.listErr != nil {
		return nil, g.listErr
	}
	if all || limit >= len(g.listTags) {
		return append([]string(nil), g.listTags...), nil
	}
	return append([]string(nil), g.listTags[:limit]...), nil
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

// fakeRunner implements CommandRunner. Used for git clone (cloneFn) and
// for any other invocation (stdoutFn, e.g. `git rev-parse` for `info`).
type fakeCmdRunner struct {
	calls    []recordedCall
	cloneFn  func(args []string, dir string) error
	stdoutFn func(name string, args []string, w io.Writer)
}

type recordedCall struct {
	Name string
	Args []string
	Dir  string
}

func (r *fakeCmdRunner) Run(ctx context.Context, name string, args []string, dir string, stdout, stderr io.Writer) error {
	r.calls = append(r.calls, recordedCall{Name: name, Args: append([]string(nil), args...), Dir: dir})
	if r.stdoutFn != nil {
		r.stdoutFn(name, args, stdout)
	}
	if r.cloneFn != nil && name == "git" {
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

func (s *realPathStore) ShimsDir() string {
	return filepath.Join(s.root, "shims")
}

func (s *realPathStore) BenchmarksDir() string {
	return filepath.Join(s.root, "benchmarks")
}

func newInstallDeps(t *testing.T, store Store) (*Deps, *fakeGitHub, *fakeBuilder, *fakeCmdRunner, *fakeShimInstaller) {
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
		// Mix of matching binaries (llama-*, executable), a non-matching
		// helper (export-graph-ops), and a non-executable file (libfoo.dylib
		// stub). Discovery should pick exactly the four llama-* executables.
		for _, name := range testInstalledBinaries {
			if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"), 0o755); err != nil {
				return err
			}
		}
		if err := os.WriteFile(filepath.Join(bin, "export-graph-ops"), []byte("#!/bin/sh\n"), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(bin, "libfoo.dylib"), []byte("not an executable"), 0o644); err != nil {
			return err
		}
		return nil
	}}
	si := &fakeShimInstaller{}
	deps := &Deps{
		Store:         store,
		GitHub:        g,
		Builder:       b,
		Git:           r,
		Platform:      fakePlatform{apple: true},
		ShimInstaller: si,
		Now:           func() time.Time { return time.Date(2026, 5, 2, 14, 30, 30, 0, time.UTC) },
	}
	return deps, g, b, r, si
}

func TestInstall_RequiresVersion(t *testing.T) {
	deps, _, _, _, _ := newInstallDeps(t, &fakeStore{})
	if _, _, err := runRoot(t, deps, "install"); err == nil {
		t.Fatal("expected error when version arg missing")
	}
}

func TestInstall_RefusesNonAppleSilicon(t *testing.T) {
	store := newRealPathStore(t)
	deps, _, _, _, _ := newInstallDeps(t, store)
	deps.Platform = fakePlatform{apple: false}
	_, _, err := runRoot(t, deps, "install", "b5046")
	if err == nil || !strings.Contains(err.Error(), "Apple Silicon") {
		t.Fatalf("err = %v, want one mentioning Apple Silicon", err)
	}
}

func TestInstall_AlreadyInstalledIsIdempotent(t *testing.T) {
	store := newRealPathStore(t, "b5046")
	deps, g, b, r, _ := newInstallDeps(t, store)
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
	deps, g, _, r, _ := newInstallDeps(t, store)
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
	deps, g, _, _, _ := newInstallDeps(t, store)
	g.tagErr = gh.ErrTagNotFound
	_, _, err := runRoot(t, deps, "install", "b9999")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("err = %v, want one mentioning 'not found'", err)
	}
}

func TestInstall_HappyPath(t *testing.T) {
	store := newRealPathStore(t)
	deps, _, b, r, _ := newInstallDeps(t, store)
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
	// Bin symlinks must exist in final dir, BE RELATIVE (so they survive
	// the staging→final dir rename), AND resolve to the build artifact.
	finalBin := filepath.Join(store.root, "versions", "b5046", "bin")
	for _, name := range testInstalledBinaries {
		link := filepath.Join(finalBin, name)
		fi, err := os.Lstat(link)
		if err != nil {
			t.Fatalf("expected symlink %s: %v", name, err)
		}
		if fi.Mode()&os.ModeSymlink == 0 {
			t.Errorf("%s is not a symlink (mode=%v)", name, fi.Mode())
		}
		target, err := os.Readlink(link)
		if err != nil {
			t.Fatalf("readlink %s: %v", name, err)
		}
		if filepath.IsAbs(target) {
			t.Errorf("%s symlink target %q is absolute; must be relative so it survives staging→final rename", name, target)
		}
		if _, err := os.Stat(link); err != nil {
			t.Errorf("symlink %s does not resolve in the final dir: %v", name, err)
		}
	}
	// install_name_tool must have been invoked once per relocatable binary
	// to rewrite the absolute build-dir LC_RPATH to @loader_path. cmake bakes
	// the build dir's absolute path in at link time; without this rewrite the
	// dylibs become unfindable after staging→final dir rename.
	stagingBin := filepath.Join(store.root, "versions", ".staging-b5046", "source", "build", "bin")
	wantTargets := map[string]bool{}
	for _, n := range testInstalledBinaries {
		wantTargets[filepath.Join(stagingBin, n)] = false
	}
	for _, c := range r.calls {
		if c.Name != "install_name_tool" {
			continue
		}
		if len(c.Args) < 2 {
			continue
		}
		target := c.Args[len(c.Args)-1]
		if _, ok := wantTargets[target]; ok {
			wantTargets[target] = true
		}
	}
	for target, seen := range wantTargets {
		if !seen {
			t.Errorf("expected install_name_tool to be called on %s, not seen", target)
		}
	}
	if !strings.Contains(out, "Installed b5046") {
		t.Fatalf("stdout = %q, want 'Installed b5046'", out)
	}
}

func TestInstall_HappyPathInstallsShimsOnce(t *testing.T) {
	store := newRealPathStore(t)
	deps, _, _, _, si := newInstallDeps(t, store)
	if _, _, err := runRoot(t, deps, "install", "b5046"); err != nil {
		t.Fatalf("install: %v", err)
	}
	if len(si.calls) != 1 {
		t.Fatalf("ShimInstaller calls = %d, want 1", len(si.calls))
	}
	if !strings.HasSuffix(si.calls[0], "shims") {
		t.Fatalf("ShimInstaller called with %q, want a shims dir", si.calls[0])
	}
}

func TestInstall_FailureSkipsShimInstall(t *testing.T) {
	store := newRealPathStore(t)
	deps, _, b, _, si := newInstallDeps(t, store)
	b.err = errors.New("cmake exited 1")
	_, _, err := runRoot(t, deps, "install", "b5046")
	if err == nil {
		t.Fatal("expected error")
	}
	if len(si.calls) != 0 {
		t.Fatalf("ShimInstaller called %d times on failed install, want 0", len(si.calls))
	}
}

func TestInstall_KeepsActiveOnSecondInstall(t *testing.T) {
	store := newRealPathStore(t)
	if err := store.SetActive("b5046"); err != nil {
		t.Fatal(err)
	}
	store.installed = append(store.installed, "b5046")
	deps, _, _, _, _ := newInstallDeps(t, store)
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
	deps, _, b, _, _ := newInstallDeps(t, store)
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
	deps, _, _, r, _ := newInstallDeps(t, store)
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
	deps, g, _, _, _ := newInstallDeps(t, store)
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
	deps, _, _, _, _ := newInstallDeps(t, store)
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
