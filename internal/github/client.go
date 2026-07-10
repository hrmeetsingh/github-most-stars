// Package github provides a minimal client for the GitHub REST search API,
// used unauthenticated (10 requests/min core rate limit).
package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const (
	searchURL = "https://api.github.com/search/repositories"
	userAgent = "github-most-stars-tui"
)

// Repo is a single repository result from the GitHub search API.
type Repo struct {
	FullName    string    `json:"full_name"`
	HTMLURL     string    `json:"html_url"`
	Stars       int       `json:"stargazers_count"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
}

type searchResponse struct {
	Items            []Repo `json:"items"`
	Message          string `json:"message"`
	DocumentationURL string `json:"documentation_url"`
}

// RateLimitError indicates the GitHub API rejected the request due to rate limiting.
type RateLimitError struct {
	Message string
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("github rate limited: %s", e.Message)
}

// Client is a small wrapper around http.Client for querying repo search.
type Client struct {
	http *http.Client
}

// NewClient returns a Client with a sane request timeout.
func NewClient() *Client {
	return &Client{http: &http.Client{Timeout: 15 * time.Second}}
}

// SearchRepos runs a GitHub code search query (e.g. "stars:>1") sorted by
// sort/order, returning up to perPage results from the given page (1-indexed).
func (c *Client) SearchRepos(ctx context.Context, query, sort, order string, perPage, page int) ([]Repo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	if err != nil {
		return nil, err
	}
	q := req.URL.Query()
	q.Set("q", query)
	q.Set("sort", sort)
	q.Set("order", order)
	q.Set("per_page", fmt.Sprintf("%d", perPage))
	q.Set("page", fmt.Sprintf("%d", page))
	req.URL.RawQuery = q.Encode()

	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", userAgent)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var body searchResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests {
		return nil, &RateLimitError{Message: body.Message}
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github search failed: %d %s", resp.StatusCode, body.Message)
	}

	return body.Items, nil
}
