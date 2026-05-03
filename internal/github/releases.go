// Package github is a tiny REST client for the llama.cpp releases endpoint.
package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"os"
	"time"
)

const defaultBaseURL = "https://api.github.com"

// Sentinel errors.
var (
	ErrTagNotFound = errors.New("tag not found")
	ErrRateLimited = errors.New("github rate limited")
)

// Client queries the public llama.cpp release endpoints.
type Client struct {
	BaseURL string
	HTTP    *http.Client
	Token   string // optional; falls back to GITHUB_TOKEN env at New()
}

// New returns a Client with sensible defaults.
func New() *Client {
	return &Client{
		BaseURL: defaultBaseURL,
		HTTP:    &http.Client{Timeout: 15 * time.Second},
		Token:   os.Getenv("GITHUB_TOKEN"),
	}
}

type release struct {
	TagName string `json:"tag_name"`
}

// Latest resolves the most recent release tag (e.g. "b5489").
func (c *Client) Latest(ctx context.Context) (string, error) {
	url := c.BaseURL + "/repos/ggml-org/llama.cpp/releases/latest"
	body, err := c.get(ctx, url)
	if err != nil {
		return "", err
	}
	var r release
	if err := json.Unmarshal(body, &r); err != nil {
		return "", fmt.Errorf("decode latest: %w", err)
	}
	if r.TagName == "" {
		return "", fmt.Errorf("github: empty tag_name in response")
	}
	return r.TagName, nil
}

// ListReleases returns release tags newest-first. When all is true the
// limit is ignored and the client paginates until GitHub returns a short
// page (signalling end-of-list). When all is false the request fetches a
// single page sized to the limit so we don't waste bandwidth on entries
// the caller will discard.
func (c *Client) ListReleases(ctx context.Context, limit int, all bool) ([]string, error) {
	const maxPerPage = 100
	perPage := maxPerPage
	if !all && limit < perPage {
		perPage = limit
	}
	var tags []string
	for page := 1; ; page++ {
		url := fmt.Sprintf("%s/repos/ggml-org/llama.cpp/releases?per_page=%d&page=%d",
			c.BaseURL, perPage, page)
		body, err := c.get(ctx, url)
		if err != nil {
			return nil, err
		}
		var rels []release
		if err := json.Unmarshal(body, &rels); err != nil {
			return nil, fmt.Errorf("decode releases page %d: %w", page, err)
		}
		for _, r := range rels {
			tags = append(tags, r.TagName)
			if !all && len(tags) >= limit {
				return tags, nil
			}
		}
		// Short page signals last page (GitHub fills full pages while
		// more results remain).
		if len(rels) < perPage {
			return tags, nil
		}
	}
}

// TagExists returns nil if the tag exists, ErrTagNotFound on 404,
// ErrRateLimited when GitHub reports the request as rate-limited, or another
// error for other failures.
func (c *Client) TagExists(ctx context.Context, tag string) error {
	url := c.BaseURL + "/repos/ggml-org/llama.cpp/releases/tags/" + neturl.PathEscape(tag)
	_, err := c.get(ctx, url)
	return err
}

func (c *Client) get(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github request: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	switch resp.StatusCode {
	case http.StatusOK:
		return body, nil
	case http.StatusNotFound:
		return nil, ErrTagNotFound
	case http.StatusForbidden, http.StatusTooManyRequests:
		if resp.Header.Get("X-RateLimit-Remaining") == "0" {
			return nil, ErrRateLimited
		}
		return nil, fmt.Errorf("github %d: %s", resp.StatusCode, body)
	default:
		return nil, fmt.Errorf("github %d: %s", resp.StatusCode, body)
	}
}
