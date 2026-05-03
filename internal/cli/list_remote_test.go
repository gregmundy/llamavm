package cli

import (
	"errors"
	"strings"
	"testing"

	gh "github.com/gregmundy/llamavm/internal/github"
)

func TestListRemote_DefaultLimitIs30(t *testing.T) {
	g := &fakeGitHub{listTags: []string{"b9010", "b9009", "b9008"}}
	deps := &Deps{GitHub: g}
	out, _, err := runRoot(t, deps, "list-remote")
	if err != nil {
		t.Fatalf("list-remote: %v", err)
	}
	if g.listLastArgs.limit != 30 || g.listLastArgs.all {
		t.Fatalf("ListReleases args = {limit:%d all:%v}, want {30 false}",
			g.listLastArgs.limit, g.listLastArgs.all)
	}
	want := "b9010 (latest)\nb9009\nb9008\n"
	if out != want {
		t.Fatalf("stdout = %q, want %q", out, want)
	}
}

func TestListRemote_LimitFlag(t *testing.T) {
	g := &fakeGitHub{listTags: []string{"b9010", "b9009", "b9008", "b9007", "b9006"}}
	deps := &Deps{GitHub: g}
	out, _, err := runRoot(t, deps, "list-remote", "--limit", "2")
	if err != nil {
		t.Fatalf("list-remote --limit 2: %v", err)
	}
	if g.listLastArgs.limit != 2 {
		t.Fatalf("ListReleases limit = %d, want 2", g.listLastArgs.limit)
	}
	want := "b9010 (latest)\nb9009\n"
	if out != want {
		t.Fatalf("stdout = %q, want %q", out, want)
	}
}

func TestListRemote_AllFlag(t *testing.T) {
	g := &fakeGitHub{listTags: []string{"b1", "b2"}}
	deps := &Deps{GitHub: g}
	if _, _, err := runRoot(t, deps, "list-remote", "--all"); err != nil {
		t.Fatalf("list-remote --all: %v", err)
	}
	if !g.listLastArgs.all {
		t.Fatal("ListReleases all = false, want true when --all is passed")
	}
}

func TestListRemote_NegativeLimitIsUserError(t *testing.T) {
	deps := &Deps{GitHub: &fakeGitHub{}}
	_, _, err := runRoot(t, deps, "list-remote", "--limit", "0")
	if err == nil {
		t.Fatal("expected error when --limit < 1")
	}
	if !errors.Is(err, ErrUserError) {
		t.Fatalf("err = %v, want ErrUserError chain", err)
	}
}

func TestListRemote_RateLimitedSurfacesGitHubTokenHint(t *testing.T) {
	deps := &Deps{GitHub: &fakeGitHub{listErr: gh.ErrRateLimited}}
	_, _, err := runRoot(t, deps, "list-remote")
	if err == nil {
		t.Fatal("expected error when github rate-limits")
	}
	if !errors.Is(err, ErrUserError) {
		t.Fatalf("err = %v, want ErrUserError chain", err)
	}
	if !strings.Contains(err.Error(), "GITHUB_TOKEN") {
		t.Fatalf("err = %v, want it to mention GITHUB_TOKEN", err)
	}
}

func TestListRemote_EmptyResult(t *testing.T) {
	g := &fakeGitHub{listTags: nil}
	deps := &Deps{GitHub: g}
	out, _, err := runRoot(t, deps, "list-remote")
	if err != nil {
		t.Fatalf("list-remote: %v", err)
	}
	if out != "" {
		t.Fatalf("stdout = %q, want empty", out)
	}
}
