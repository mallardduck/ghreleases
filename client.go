package ghrelease

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"
)

// NewClient creates a GitHub API client.
// Token is optional but recommended to avoid rate limits.
// If token is empty, tries GITHUB_TOKEN environment variable.
func NewClient(token string) *Client {
	if token == "" {
		token = os.Getenv("GITHUB_TOKEN")
	}

	return &Client{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		token:   token,
		baseURL: "https://api.github.com",
	}
}

// NewClientWithHTTP creates a client with a custom HTTP client.
// Allows callers to configure timeouts, proxies, etc.
func NewClientWithHTTP(httpClient *http.Client, token string) *Client {
	if token == "" {
		token = os.Getenv("GITHUB_TOKEN")
	}

	return &Client{
		httpClient: httpClient,
		token:      token,
		baseURL:    "https://api.github.com",
	}
}

// LatestRelease fetches the latest release tag for a repository.
func (c *Client) LatestRelease(ctx context.Context, owner, repo string) (string, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/releases/latest", c.baseURL, owner, repo)

	var release struct {
		TagName string `json:"tag_name"`
	}

	if err := c.doGet(ctx, url, &release); err != nil {
		return "", fmt.Errorf("%w: %v", ErrReleaseNotFound, err)
	}

	return release.TagName, nil
}

// GetRelease fetches a specific release by tag.
func (c *Client) GetRelease(ctx context.Context, owner, repo, tag string) (*Release, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/releases/tags/%s", c.baseURL, owner, repo, tag)

	var releaseResp struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name string `json:"name"`
			URL  string `json:"url"`
			Size int64  `json:"size"`
		} `json:"assets"`
	}

	if err := c.doGet(ctx, url, &releaseResp); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrReleaseNotFound, err)
	}

	release := &Release{
		TagName: releaseResp.TagName,
		Assets:  make([]Asset, len(releaseResp.Assets)),
	}

	for i, asset := range releaseResp.Assets {
		release.Assets[i] = Asset{
			Name: asset.Name,
			URL:  asset.URL,
			Size: asset.Size,
		}
	}

	return release, nil
}

// FetchChecksums downloads and parses a checksum file.
func (c *Client) FetchChecksums(ctx context.Context, url string) (map[string]string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	req.Header.Set("Accept", "application/octet-stream")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	return ParseChecksumFile(resp.Body)
}

func (c *Client) doGet(ctx context.Context, url string, v interface{}) error {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}

	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	return json.NewDecoder(resp.Body).Decode(v)
}
