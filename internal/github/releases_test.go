package github

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClient_Latest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/ggml-org/llama.cpp/releases/latest" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"tag_name":"b5489","name":"b5489"}`))
	}))
	defer srv.Close()

	c := New()
	c.BaseURL = srv.URL
	got, err := c.Latest(context.Background())
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if got != "b5489" {
		t.Fatalf("Latest = %q, want b5489", got)
	}
}

func TestClient_ListReleases_Limit(t *testing.T) {
	// Server returns 100 tags but the client requests only 5; the URL's
	// per_page should equal the limit when limit < 100 so we don't waste
	// bandwidth fetching unused entries.
	var seenPerPage string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/ggml-org/llama.cpp/releases" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		seenPerPage = r.URL.Query().Get("per_page")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[
			{"tag_name":"b9010"},
			{"tag_name":"b9009"},
			{"tag_name":"b9008"},
			{"tag_name":"b9007"},
			{"tag_name":"b9006"}
		]`))
	}))
	defer srv.Close()

	c := New()
	c.BaseURL = srv.URL
	got, err := c.ListReleases(context.Background(), 5, false)
	if err != nil {
		t.Fatalf("ListReleases: %v", err)
	}
	want := []string{"b9010", "b9009", "b9008", "b9007", "b9006"}
	if len(got) != len(want) {
		t.Fatalf("got %d tags, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	if seenPerPage != "5" {
		t.Errorf("per_page = %q, want %q (limit should bound page size)", seenPerPage, "5")
	}
}

func TestClient_ListReleases_Pagination(t *testing.T) {
	// --all collects across pages until a short page signals end-of-list.
	var seenPages []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPages = append(seenPages, r.URL.Query().Get("page"))
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("page") {
		case "1":
			// Full page of 100 (use synthetic shortened representation).
			w.Write([]byte(`[` + repeatTag("b9010", 100) + `]`))
		case "2":
			// Short page (3 entries) → signals last page.
			w.Write([]byte(`[{"tag_name":"a1"},{"tag_name":"a2"},{"tag_name":"a3"}]`))
		default:
			t.Errorf("unexpected page %q", r.URL.Query().Get("page"))
		}
	}))
	defer srv.Close()

	c := New()
	c.BaseURL = srv.URL
	got, err := c.ListReleases(context.Background(), 0, true)
	if err != nil {
		t.Fatalf("ListReleases all: %v", err)
	}
	if len(got) != 103 {
		t.Fatalf("got %d tags, want 103 (100 + 3)", len(got))
	}
	if len(seenPages) != 2 || seenPages[0] != "1" || seenPages[1] != "2" {
		t.Errorf("pages requested = %v, want [1 2]", seenPages)
	}
}

// repeatTag emits n copies of a JSON release object joined by commas.
func repeatTag(tag string, n int) string {
	out := ""
	for i := 0; i < n; i++ {
		if i > 0 {
			out += ","
		}
		out += `{"tag_name":"` + tag + `"}`
	}
	return out
}

func TestClient_ListReleases_RateLimited(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	c := New()
	c.BaseURL = srv.URL
	_, err := c.ListReleases(context.Background(), 30, false)
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("err = %v, want ErrRateLimited", err)
	}
}

func TestClient_Latest_BadStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := New()
	c.BaseURL = srv.URL
	if _, err := c.Latest(context.Background()); err == nil {
		t.Fatal("expected error on 500")
	}
}

func TestClient_TagExists_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/ggml-org/llama.cpp/releases/tags/b5046" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"tag_name":"b5046"}`))
	}))
	defer srv.Close()

	c := New()
	c.BaseURL = srv.URL
	if err := c.TagExists(context.Background(), "b5046"); err != nil {
		t.Fatalf("TagExists: %v", err)
	}
}

func TestClient_TagExists_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	c := New()
	c.BaseURL = srv.URL
	err := c.TagExists(context.Background(), "bogus")
	if !errors.Is(err, ErrTagNotFound) {
		t.Fatalf("TagExists = %v, want ErrTagNotFound", err)
	}
}

func TestClient_Latest_MalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("not json"))
	}))
	defer srv.Close()

	c := New()
	c.BaseURL = srv.URL
	if _, err := c.Latest(context.Background()); err == nil {
		t.Fatal("expected decode error on malformed JSON")
	}
}

func TestClient_RateLimited(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "0")
		http.Error(w, "rate limited", http.StatusForbidden)
	}))
	defer srv.Close()

	c := New()
	c.BaseURL = srv.URL
	_, err := c.Latest(context.Background())
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("Latest = %v, want ErrRateLimited", err)
	}
}

func TestClient_TagExists_RateLimited(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "0")
		http.Error(w, "rate limited", http.StatusForbidden)
	}))
	defer srv.Close()

	c := New()
	c.BaseURL = srv.URL
	if err := c.TagExists(context.Background(), "b5046"); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("TagExists = %v, want ErrRateLimited", err)
	}
}

func TestClient_Forbidden_NotRateLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 403 without X-RateLimit-Remaining: 0 → must NOT be classified as ErrRateLimited.
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer srv.Close()

	c := New()
	c.BaseURL = srv.URL
	_, err := c.Latest(context.Background())
	if err == nil {
		t.Fatal("expected error on 403")
	}
	if errors.Is(err, ErrRateLimited) {
		t.Fatalf("403 without remaining=0 should not be ErrRateLimited, got %v", err)
	}
}

func TestClient_TokenHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer testtoken" {
			t.Errorf("Authorization = %q, want Bearer testtoken", got)
		}
		w.Write([]byte(`{"tag_name":"b1"}`))
	}))
	defer srv.Close()

	c := New()
	c.BaseURL = srv.URL
	c.Token = "testtoken"
	if _, err := c.Latest(context.Background()); err != nil {
		t.Fatalf("Latest: %v", err)
	}
}
