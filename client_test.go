package ghrelease

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
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
