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
