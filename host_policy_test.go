package ghrelease

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHostPolicy(t *testing.T) {
	c := NewClient("tok", WithBaseURL("https://ghe.example.com/api/v3"))
	tokenYes := []string{"ghe.example.com", "github.com", "api.github.com", "objects.githubusercontent.com", "githubusercontent.com", "GitHub.com"}
	tokenNo := []string{"evilgithub.com", "github.com.evil.com", "notgithubusercontent.com", "example.com", "127.0.0.1"}
	for _, h := range tokenYes {
		if !c.tokenHost(h) {
			t.Errorf("tokenHost(%q) = false", h)
		}
	}
	for _, h := range tokenNo {
		if c.tokenHost(h) {
			t.Errorf("tokenHost(%q) = true", h)
		}
	}
}

func TestTokenScopingHeader(t *testing.T) {
	c := NewClient("tok")
	tests := []struct {
		url      string
		wantAuth bool
	}{
		{"https://api.github.com/repos/a/b", true},
		{"https://github.com/a/b/releases/download/v1/x", true},
		{"https://objects.githubusercontent.com/x", true},
		{"https://githubusercontent.com/x", true},
		{"https://evilgithub.com/x", false},
		{"https://github.com.evil.com/x", false},
		{"https://example.com/x", false},
	}
	for _, tt := range tests {
		req, err := c.newRequest(context.Background(), tt.url, "*/*")
		if err != nil {
			t.Fatal(err)
		}
		if got := req.Header.Get("Authorization") != ""; got != tt.wantAuth {
			t.Errorf("%s: Authorization present = %v, want %v", tt.url, got, tt.wantAuth)
		}
	}

	// The API base host (e.g. an httptest server) always receives the token.
	req, _ := NewClient("tok", WithBaseURL("http://127.0.0.1:1234")).newRequest(context.Background(), "http://127.0.0.1:1234/x", "*/*")
	if req.Header.Get("Authorization") != "Bearer tok" {
		t.Error("base host did not receive token")
	}
}

func TestTokenNotSentToOtherHostOnDownload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Errorf("token leaked to %s", r.Host)
		}
		_, _ = io.WriteString(w, "ok")
	}))
	defer srv.Close()

	// srv is neither the base URL nor a GitHub host.
	if _, err := NewClient("secret").Download(srv.URL, io.Discard, DownloadOptions{}); err != nil {
		t.Fatal(err)
	}
}

func TestAllowedHosts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "ok") }))
	defer srv.Close()

	c := NewClient("", WithBaseURL(srv.URL), WithAllowedHosts("github.com"))

	if _, err := c.Download(srv.URL, io.Discard, DownloadOptions{}); err != nil {
		t.Errorf("base URL host must always be allowed: %v", err)
	}
	_, err := c.Download("https://example.com/x", io.Discard, DownloadOptions{})
	if !errors.Is(err, ErrHostNotAllowed) {
		t.Errorf("error = %v, want ErrHostNotAllowed", err)
	}
	_, err = c.FetchChecksums(context.Background(), "https://example.com/x")
	if !errors.Is(err, ErrHostNotAllowed) {
		t.Errorf("FetchChecksums error = %v, want ErrHostNotAllowed", err)
	}

	for host, want := range map[string]bool{
		"github.com": true, "api.github.com": true, "evilgithub.com": false, "example.com": false,
	} {
		if got := c.hostAllowed(host); got != want {
			t.Errorf("hostAllowed(%q) = %v, want %v", host, got, want)
		}
	}
	if !NewClient("").hostAllowed("anything.example") {
		t.Error("default client must be unrestricted")
	}
}

func TestAllowedHostsEnforcedOnRedirect(t *testing.T) {
	var hits int
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		_, _ = io.WriteString(w, "secret data")
	}))
	defer other.Close()
	// Redirect to a different host name (localhost vs 127.0.0.1) so the allowlist applies.
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, strings.Replace(other.URL, "127.0.0.1", "localhost", 1), http.StatusFound)
	}))
	defer redirector.Close()

	c := NewClient("tok", WithBaseURL(redirector.URL), WithAllowedHosts("github.com"))
	_, err := c.Download(redirector.URL, io.Discard, DownloadOptions{})
	if !errors.Is(err, ErrHostNotAllowed) {
		t.Fatalf("error = %v, want ErrHostNotAllowed", err)
	}
	if hits != 0 {
		t.Error("redirect target was contacted")
	}

	// Allowing the target host lets the same redirect through.
	c = NewClient("tok", WithBaseURL(redirector.URL), WithAllowedHosts("localhost"))
	if _, err := c.Download(redirector.URL, io.Discard, DownloadOptions{}); err != nil {
		t.Errorf("allowed redirect failed: %v", err)
	}
}

func TestRedirectDropsTokenForNonGitHubHost(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("Authorization survived redirect to non-GitHub host")
		}
		_, _ = io.WriteString(w, "x")
	}))
	defer target.Close()
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Error("base host did not get token")
		}
		http.Redirect(w, r, strings.Replace(target.URL, "127.0.0.1", "localhost", 1), http.StatusFound)
	}))
	defer redirector.Close()

	c := NewClient("tok", WithBaseURL(redirector.URL))
	if _, err := c.Download(redirector.URL, io.Discard, DownloadOptions{}); err != nil {
		t.Fatal(err)
	}
}

func TestSendDoesNotMutateCallerClient(t *testing.T) {
	hc := &http.Client{}
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	defer srv.Close()
	c := NewClientWithHTTP(hc, "", WithBaseURL(srv.URL))
	_, _ = c.Download(srv.URL, io.Discard, DownloadOptions{})
	if hc.CheckRedirect != nil {
		t.Error("caller's http.Client was mutated")
	}
}
