package ghrelease

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func TestNewClient(t *testing.T) {
	// Test without token
	client := NewClient("")
	if client.token != "" && client.token != os.Getenv("GITHUB_TOKEN") {
		t.Errorf("NewClient() token = %v, want empty or GITHUB_TOKEN env", client.token)
	}

	// Test with explicit token
	client = NewClient("test-token")
	if client.token != "test-token" {
		t.Errorf("NewClient() token = %v, want %v", client.token, "test-token")
	}

	if client.baseURL != "https://api.github.com" {
		t.Errorf("NewClient() baseURL = %v, want %v", client.baseURL, "https://api.github.com")
	}
}

func TestNewClientWithHTTP(t *testing.T) {
	httpClient := &http.Client{}
	client := NewClientWithHTTP(httpClient, "custom-token")

	if client.httpClient != httpClient {
		t.Errorf("NewClientWithHTTP() httpClient not preserved")
	}

	if client.token != "custom-token" {
		t.Errorf("NewClientWithHTTP() token = %v, want %v", client.token, "custom-token")
	}
}

func TestLatestRelease(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		response   string
		wantTag    string
		wantErr    error
	}{
		{
			name:       "successful fetch",
			statusCode: http.StatusOK,
			response:   `{"tag_name": "v1.2.3"}`,
			wantTag:    "v1.2.3",
			wantErr:    nil,
		},
		{
			name:       "release not found",
			statusCode: http.StatusNotFound,
			response:   `{"message": "Not Found"}`,
			wantErr:    ErrReleaseNotFound,
		},
		{
			name:       "invalid JSON",
			statusCode: http.StatusOK,
			response:   `invalid json`,
			wantErr:    ErrReleaseNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Verify request
				if r.Method != "GET" {
					t.Errorf("Expected GET request, got %s", r.Method)
				}
				if r.Header.Get("Accept") != "application/vnd.github.v3+json" {
					t.Errorf("Expected GitHub API Accept header")
				}

				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(tt.response))
			}))
			defer server.Close()

			client := NewClient("")
			client.baseURL = server.URL

			tag, err := client.LatestRelease(context.Background(), "owner", "repo")

			if tt.wantErr != nil {
				if err == nil {
					t.Errorf("LatestRelease() error = nil, wantErr %v", tt.wantErr)
					return
				}
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("LatestRelease() error = %v, wantErr %v", err, tt.wantErr)
				}
				return
			}

			if err != nil {
				t.Errorf("LatestRelease() unexpected error = %v", err)
				return
			}

			if tag != tt.wantTag {
				t.Errorf("LatestRelease() tag = %v, want %v", tag, tt.wantTag)
			}
		})
	}
}

func TestLatestReleaseWithAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-token" {
			t.Errorf("Expected Authorization header 'Bearer test-token', got %s", auth)
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"tag_name": "v1.0.0"}`))
	}))
	defer server.Close()

	client := NewClient("test-token")
	client.baseURL = server.URL

	_, err := client.LatestRelease(context.Background(), "owner", "repo")
	if err != nil {
		t.Errorf("LatestRelease() unexpected error = %v", err)
	}
}

func TestGetRelease(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		response   string
		wantAssets int
		wantErr    error
	}{
		{
			name:       "successful fetch with assets",
			statusCode: http.StatusOK,
			response: `{
				"tag_name": "v1.0.0",
				"assets": [
					{"name": "binary-linux-amd64", "url": "https://api.github.com/asset/1", "size": 1024},
					{"name": "binary-darwin-amd64", "url": "https://api.github.com/asset/2", "size": 2048}
				]
			}`,
			wantAssets: 2,
			wantErr:    nil,
		},
		{
			name:       "release with no assets",
			statusCode: http.StatusOK,
			response:   `{"tag_name": "v1.0.0", "assets": []}`,
			wantAssets: 0,
			wantErr:    nil,
		},
		{
			name:       "release not found",
			statusCode: http.StatusNotFound,
			response:   `{"message": "Not Found"}`,
			wantErr:    ErrReleaseNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(tt.response))
			}))
			defer server.Close()

			client := NewClient("")
			client.baseURL = server.URL

			release, err := client.GetRelease(context.Background(), "owner", "repo", "v1.0.0")

			if tt.wantErr != nil {
				if err == nil {
					t.Errorf("GetRelease() error = nil, wantErr %v", tt.wantErr)
					return
				}
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("GetRelease() error = %v, wantErr %v", err, tt.wantErr)
				}
				return
			}

			if err != nil {
				t.Errorf("GetRelease() unexpected error = %v", err)
				return
			}

			if len(release.Assets) != tt.wantAssets {
				t.Errorf("GetRelease() assets count = %d, want %d", len(release.Assets), tt.wantAssets)
			}

			if tt.wantAssets > 0 {
				// Verify first asset structure
				if release.Assets[0].Name == "" {
					t.Errorf("GetRelease() asset name is empty")
				}
				if release.Assets[0].URL == "" {
					t.Errorf("GetRelease() asset URL is empty")
				}
				if release.Assets[0].Size == 0 {
					t.Errorf("GetRelease() asset size is 0")
				}
			}
		})
	}
}

func TestFetchChecksums(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		response   string
		wantCount  int
		wantErr    bool
	}{
		{
			name:       "valid checksums file",
			statusCode: http.StatusOK,
			response: `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855  file1.txt
abc0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855  file2.txt`,
			wantCount: 2,
			wantErr:   false,
		},
		{
			name:       "empty checksums file",
			statusCode: http.StatusOK,
			response:   "",
			wantCount:  0,
			wantErr:    false,
		},
		{
			name:       "not found",
			statusCode: http.StatusNotFound,
			response:   "Not Found",
			wantErr:    true,
		},
		{
			name:       "invalid checksum format",
			statusCode: http.StatusOK,
			response:   "invalid checksum format",
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Verify headers
				if r.Header.Get("Accept") != "application/octet-stream" {
					t.Errorf("Expected Accept: application/octet-stream header")
				}

				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(tt.response))
			}))
			defer server.Close()

			client := NewClient("")
			checksums, err := client.FetchChecksums(context.Background(), server.URL)

			if tt.wantErr {
				if err == nil {
					t.Errorf("FetchChecksums() error = nil, wantErr true")
				}
				return
			}

			if err != nil {
				t.Errorf("FetchChecksums() unexpected error = %v", err)
				return
			}

			if len(checksums) != tt.wantCount {
				t.Errorf("FetchChecksums() count = %d, want %d", len(checksums), tt.wantCount)
			}
		})
	}
}

func TestListReleases(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		response   string
		opts       *ListOptions
		wantLen    int
		wantErr    error
		wantURL    string // expected query string suffix
	}{
		{
			name:       "returns multiple releases",
			statusCode: http.StatusOK,
			response: `[
				{"id":1,"tag_name":"v2.0.0","name":"Release 2","draft":false,"prerelease":false,"published_at":"2026-02-01T00:00:00Z","assets":[]},
				{"id":2,"tag_name":"v1.0.0","name":"Release 1","draft":false,"prerelease":false,"published_at":"2026-01-01T00:00:00Z","assets":[]}
			]`,
			wantLen: 2,
		},
		{
			name:       "empty list",
			statusCode: http.StatusOK,
			response:   `[]`,
			wantLen:    0,
		},
		{
			name:       "api error",
			statusCode: http.StatusNotFound,
			response:   `{"message":"Not Found"}`,
			wantErr:    ErrReleaseNotFound,
		},
		{
			name:       "pagination options forwarded",
			statusCode: http.StatusOK,
			response:   `[]`,
			opts:       &ListOptions{Page: 3, PerPage: 50},
			wantURL:    "page=3&per_page=50",
			wantLen:    0,
		},
		{
			name:       "per_page capped at 100",
			statusCode: http.StatusOK,
			response:   `[]`,
			opts:       &ListOptions{PerPage: 999},
			wantURL:    "page=1&per_page=100",
			wantLen:    0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tt.wantURL != "" && r.URL.RawQuery != tt.wantURL {
					t.Errorf("ListReleases() query = %q, want %q", r.URL.RawQuery, tt.wantURL)
				}
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(tt.response))
			}))
			defer server.Close()

			client := NewClient("")
			client.baseURL = server.URL

			releases, err := client.ListReleases(context.Background(), "owner", "repo", tt.opts)

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("ListReleases() error = %v, wantErr %v", err, tt.wantErr)
				}
				return
			}

			if err != nil {
				t.Errorf("ListReleases() unexpected error = %v", err)
				return
			}

			if len(releases) != tt.wantLen {
				t.Fatalf("ListReleases() len = %d, want %d", len(releases), tt.wantLen)
			}
		})
	}
}

func TestListReleases_fieldsPopulated(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[{
			"id": 42,
			"tag_name": "v1.0.0",
			"name": "First Release",
			"draft": true,
			"prerelease": true,
			"published_at": "2026-01-15T12:00:00Z",
			"assets": [{"name": "binary.tar.gz", "url": "https://example.com/asset/1", "size": 1024}]
		}]`))
	}))
	defer server.Close()

	client := NewClient("")
	client.baseURL = server.URL

	releases, err := client.ListReleases(context.Background(), "owner", "repo", nil)
	if err != nil {
		t.Fatalf("ListReleases() unexpected error = %v", err)
	}
	if len(releases) != 1 {
		t.Fatalf("ListReleases() len = %d, want 1", len(releases))
	}

	r := releases[0]
	if r.ID != 42 {
		t.Errorf("ID = %d, want 42", r.ID)
	}
	if r.TagName != "v1.0.0" {
		t.Errorf("TagName = %q, want v1.0.0", r.TagName)
	}
	if r.Name != "First Release" {
		t.Errorf("Name = %q, want First Release", r.Name)
	}
	if !r.Draft {
		t.Errorf("Draft = false, want true")
	}
	if !r.Prerelease {
		t.Errorf("Prerelease = false, want true")
	}
	if r.PublishedAt.IsZero() {
		t.Errorf("PublishedAt is zero")
	}
	if len(r.Assets) != 1 || r.Assets[0].Name != "binary.tar.gz" {
		t.Errorf("Assets not populated correctly")
	}
}

func TestListAllReleases(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		switch r.URL.Query().Get("page") {
		case "1":
			// Return a full page of 100 items (abbreviated — just use 2 for the test)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[{"id":1,"tag_name":"v2.0.0","published_at":"2026-02-01T00:00:00Z","assets":[]},{"id":2,"tag_name":"v1.0.0","published_at":"2026-01-01T00:00:00Z","assets":[]}]`))
		default:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[]`))
		}
	}))
	defer server.Close()

	client := NewClient("")
	client.baseURL = server.URL

	releases, err := client.ListAllReleases(context.Background(), "owner", "repo", nil)
	if err != nil {
		t.Fatalf("ListAllReleases() unexpected error = %v", err)
	}
	if len(releases) != 2 {
		t.Errorf("ListAllReleases() len = %d, want 2", len(releases))
	}
}

func TestRateLimitError(t *testing.T) {
	t.Run("HTTP 429 returns RateLimitError", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Retry-After", "30")
			w.WriteHeader(http.StatusTooManyRequests)
		}))
		defer server.Close()

		client := NewClient("")
		client.baseURL = server.URL

		_, err := client.LatestRelease(context.Background(), "owner", "repo")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !errors.Is(err, ErrRateLimited) {
			t.Errorf("errors.Is(err, ErrRateLimited) = false, want true; err = %v", err)
		}
		var rl *RateLimitError
		if !errors.As(err, &rl) {
			t.Fatalf("errors.As(*RateLimitError) = false")
		}
		if rl.RetryAfter.IsZero() {
			t.Errorf("RateLimitError.RetryAfter is zero, want non-zero from Retry-After header")
		}
	})

	t.Run("HTTP 403 with X-RateLimit-Remaining 0 returns RateLimitError", func(t *testing.T) {
		resetTime := time.Now().Add(60 * time.Second).Unix()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("X-RateLimit-Remaining", "0")
			w.Header().Set("X-RateLimit-Reset", fmt.Sprintf("%d", resetTime))
			w.WriteHeader(http.StatusForbidden)
		}))
		defer server.Close()

		client := NewClient("")
		client.baseURL = server.URL

		_, err := client.LatestRelease(context.Background(), "owner", "repo")
		if !errors.Is(err, ErrRateLimited) {
			t.Errorf("errors.Is(err, ErrRateLimited) = false; err = %v", err)
		}
		var rl *RateLimitError
		if errors.As(err, &rl) && rl.RetryAfter.IsZero() {
			t.Errorf("RateLimitError.RetryAfter is zero, want time parsed from X-RateLimit-Reset")
		}
	})

	t.Run("HTTP 403 without rate-limit header is not ErrRateLimited", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusForbidden)
		}))
		defer server.Close()

		client := NewClient("")
		client.baseURL = server.URL

		_, err := client.LatestRelease(context.Background(), "owner", "repo")
		if errors.Is(err, ErrRateLimited) {
			t.Errorf("plain 403 should not be ErrRateLimited")
		}
	})

	t.Run("ListAllReleases propagates RateLimitError", func(t *testing.T) {
		callCount := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			callCount++
			if callCount == 1 {
				// First page succeeds, returning a full page so loop continues
				items := `[`
				for i := 0; i < 99; i++ {
					items += `{"id":` + fmt.Sprintf("%d", i) + `,"tag_name":"v1.0.0","draft":false,"prerelease":false,"published_at":"2026-06-01T00:00:00Z","assets":[]},`
				}
				items += `{"id":99,"tag_name":"v1.0.0","draft":false,"prerelease":false,"published_at":"2026-06-01T00:00:00Z","assets":[]}]`
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(items))
				return
			}
			// Second page is rate-limited
			w.Header().Set("Retry-After", "60")
			w.WriteHeader(http.StatusTooManyRequests)
		}))
		defer server.Close()

		client := NewClient("")
		client.baseURL = server.URL

		_, err := client.ListAllReleases(context.Background(), "owner", "repo", nil)
		if !errors.Is(err, ErrRateLimited) {
			t.Errorf("ListAllReleases() should propagate ErrRateLimited; got %v", err)
		}
	})
}

func TestContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		// Never respond, force timeout
		<-r.Context().Done()
	}))
	defer server.Close()

	client := NewClient("")
	client.baseURL = server.URL

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := client.LatestRelease(ctx, "owner", "repo")
	if err == nil {
		t.Errorf("LatestRelease() expected error due to cancelled context")
	}
}

func TestLatestReleaseWithPrefix(t *testing.T) {
	tests := []struct {
		name      string
		prefix    string
		pages     map[string]string // page number → JSON response
		wantTag   string
		wantErr   error
		wantPages int // how many pages should be requested
	}{
		{
			name:   "match on first page",
			prefix: "sub-pkg/",
			pages: map[string]string{
				"1": `[
					{"id":1,"tag_name":"v2.0.0","draft":false,"prerelease":false,"published_at":"2026-02-01T00:00:00Z","assets":[]},
					{"id":2,"tag_name":"sub-pkg/v1.0.0","draft":false,"prerelease":false,"published_at":"2026-01-01T00:00:00Z","assets":[]}
				]`,
			},
			wantTag:   "sub-pkg/v1.0.0",
			wantPages: 1,
		},
		{
			name:   "match on second page",
			prefix: "sub-pkg/",
			pages: map[string]string{
				"1": func() string {
					// Build a 100-item page with no sub-pkg/ matches
					items := `[`
					for i := 0; i < 100; i++ {
						if i > 0 {
							items += ","
						}
						items += `{"id":` + fmt.Sprintf("%d", i) + `,"tag_name":"v1.0.0","draft":false,"prerelease":false,"published_at":"2026-02-01T00:00:00Z","assets":[]}`
					}
					return items + `]`
				}(),
				"2": `[{"id":200,"tag_name":"sub-pkg/v3.0.0","draft":false,"prerelease":false,"published_at":"2026-01-01T00:00:00Z","assets":[]}]`,
			},
			wantTag:   "sub-pkg/v3.0.0",
			wantPages: 2,
		},
		{
			name:   "no match returns ErrReleaseNotFound",
			prefix: "sub-pkg/",
			pages: map[string]string{
				"1": `[{"id":1,"tag_name":"v1.0.0","draft":false,"prerelease":false,"published_at":"2026-01-01T00:00:00Z","assets":[]}]`,
			},
			wantErr: ErrReleaseNotFound,
		},
		{
			name:   "drafts and prereleases skipped",
			prefix: "sub-pkg/",
			pages: map[string]string{
				"1": `[
					{"id":1,"tag_name":"sub-pkg/v2.0.0","draft":true,"prerelease":false,"published_at":"2026-02-01T00:00:00Z","assets":[]},
					{"id":2,"tag_name":"sub-pkg/v1.0.0-rc.1","draft":false,"prerelease":true,"published_at":"2026-01-15T00:00:00Z","assets":[]},
					{"id":3,"tag_name":"sub-pkg/v1.0.0","draft":false,"prerelease":false,"published_at":"2026-01-01T00:00:00Z","assets":[]}
				]`,
			},
			wantTag: "sub-pkg/v1.0.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pageCount := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				pageCount++
				page := r.URL.Query().Get("page")
				resp, ok := tt.pages[page]
				if !ok {
					resp = `[]`
				}
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(resp))
			}))
			defer server.Close()

			client := NewClient("")
			client.baseURL = server.URL

			ctx := WithTagPrefix(context.Background(), tt.prefix)
			tag, err := client.LatestRelease(ctx, "owner", "repo")

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("LatestReleaseWithPrefix() error = %v, wantErr %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("LatestReleaseWithPrefix() unexpected error = %v", err)
			}
			if tag != tt.wantTag {
				t.Errorf("LatestReleaseWithPrefix() = %q, want %q", tag, tt.wantTag)
			}
			if tt.wantPages > 0 && pageCount != tt.wantPages {
				t.Errorf("LatestReleaseWithPrefix() made %d page requests, want %d", pageCount, tt.wantPages)
			}
		})
	}
}

func TestListAllReleases_withOpts(t *testing.T) {
	t.Run("Since causes early exit", func(t *testing.T) {
		pageCount := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			pageCount++
			page := r.URL.Query().Get("page")
			switch page {
			case "1":
				w.WriteHeader(http.StatusOK)
				// 100 items so loop would normally continue
				items := `[`
				for i := 0; i < 99; i++ {
					items += `{"id":` + fmt.Sprintf("%d", i) + `,"tag_name":"v2.0.0","draft":false,"prerelease":false,"published_at":"2026-06-15T00:00:00Z","assets":[]},`
				}
				// Last item is before Since — should trigger early exit
				items += `{"id":99,"tag_name":"v1.0.0","draft":false,"prerelease":false,"published_at":"2026-05-01T00:00:00Z","assets":[]}]`
				_, _ = w.Write([]byte(items))
			default:
				t.Errorf("unexpected page %s requested — Since should have caused early exit", page)
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`[]`))
			}
		}))
		defer server.Close()

		client := NewClient("")
		client.baseURL = server.URL

		since := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
		releases, err := client.ListAllReleases(context.Background(), "owner", "repo", &ListOptions{
			Since: since,
		})
		if err != nil {
			t.Fatalf("ListAllReleases() unexpected error = %v", err)
		}
		if pageCount != 1 {
			t.Errorf("ListAllReleases() made %d page requests, want 1 (early exit)", pageCount)
		}
		// The item before Since should not appear in results
		for _, r := range releases {
			if r.PublishedAt.Before(since) {
				t.Errorf("ListAllReleases() included release %q published before Since", r.TagName)
			}
		}
	})

	t.Run("TagPrefix filters results", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[
				{"id":1,"tag_name":"sub-pkg/v2.0.0","draft":false,"prerelease":false,"published_at":"2026-02-01T00:00:00Z","assets":[]},
				{"id":2,"tag_name":"v1.0.0","draft":false,"prerelease":false,"published_at":"2026-01-01T00:00:00Z","assets":[]}
			]`))
		}))
		defer server.Close()

		client := NewClient("")
		client.baseURL = server.URL

		releases, err := client.ListAllReleases(context.Background(), "owner", "repo", &ListOptions{
			TagPrefix: "sub-pkg/",
		})
		if err != nil {
			t.Fatalf("ListAllReleases() unexpected error = %v", err)
		}
		if len(releases) != 1 || releases[0].TagName != "sub-pkg/v2.0.0" {
			t.Errorf("ListAllReleases() with TagPrefix got %v, want [sub-pkg/v2.0.0]", releases)
		}
	})

	t.Run("ExcludePrereleases and ExcludeDrafts", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[
				{"id":1,"tag_name":"v3.0.0","draft":false,"prerelease":false,"published_at":"2026-03-01T00:00:00Z","assets":[]},
				{"id":2,"tag_name":"v2.0.0-rc.1","draft":false,"prerelease":true,"published_at":"2026-02-01T00:00:00Z","assets":[]},
				{"id":3,"tag_name":"v1.0.0","draft":true,"prerelease":false,"published_at":"2026-01-01T00:00:00Z","assets":[]}
			]`))
		}))
		defer server.Close()

		client := NewClient("")
		client.baseURL = server.URL

		releases, err := client.ListAllReleases(context.Background(), "owner", "repo", &ListOptions{
			ExcludePrereleases: true,
			ExcludeDrafts:      true,
		})
		if err != nil {
			t.Fatalf("ListAllReleases() unexpected error = %v", err)
		}
		if len(releases) != 1 || releases[0].TagName != "v3.0.0" {
			t.Errorf("ListAllReleases() got %v, want [v3.0.0]", releases)
		}
	})
}
