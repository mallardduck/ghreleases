package ghrelease

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestLatestRelease_MultiPrefix_NoFilter_ReturnsAmbiguous documents why the
// no-filter path is ambiguous for multi-prefix repos: GitHub's /releases/latest
// returns whichever release was published most recently, regardless of tag prefix.
// In a repo like rancher/ob-charts-tool that releases both "v*" (library) and
// "cli/v*" (CLI), a caller wanting library releases gets the wrong tag if the
// CLI release was published more recently. Use WithTagPrefix to fix this.
func TestLatestRelease_MultiPrefix_NoFilter_ReturnsAmbiguous(t *testing.T) {
	// Simulate a repo where cli/v2.0.0 was published after v1.5.0.
	// GitHub's /releases/latest therefore returns cli/v2.0.0.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"tag_name": "cli/v2.0.0"}`))
	}))
	defer server.Close()

	client := NewClient("")
	client.baseURL = server.URL

	tag, err := client.LatestRelease(context.Background(), "owner", "repo")
	if err != nil {
		t.Fatal(err)
	}

	// Without a prefix filter, we get whatever GitHub considers "latest".
	// Here that is a CLI-prefixed tag even though a library caller wanted vX.Y.Z.
	// This is the ambiguity this test documents; see WithTagPrefix for the fix.
	if tag != "cli/v2.0.0" {
		t.Errorf("LatestRelease() = %q, want cli/v2.0.0 (documenting unfiltered ambiguity)", tag)
	}
}

// TestLatestRelease_WithTagPrefix_CLI asserts that passing a context with
// WithTagPrefix("cli/") causes LatestRelease to return the most recently
// published release whose tag starts with "cli/", ignoring all others.
func TestLatestRelease_WithTagPrefix_CLI(t *testing.T) {
	server := newMultiPrefixServer(t)
	defer server.Close()

	client := NewClient("")
	client.baseURL = server.URL

	ctx := WithTagPrefix(context.Background(), "cli/")
	tag, err := client.LatestRelease(ctx, "owner", "repo")
	if err != nil {
		t.Fatalf("LatestRelease() unexpected error = %v", err)
	}
	if tag != "cli/v2.0.0" {
		t.Errorf("LatestRelease() = %q, want cli/v2.0.0", tag)
	}
}

// TestLatestRelease_WithTagPrefix_Lib asserts that passing a context with
// WithTagPrefix("v") returns the latest plain vX.Y.Z release, ignoring all
// prefix-namespaced tags like "cli/v*".
func TestLatestRelease_WithTagPrefix_Lib(t *testing.T) {
	server := newMultiPrefixServer(t)
	defer server.Close()

	client := NewClient("")
	client.baseURL = server.URL

	ctx := WithTagPrefix(context.Background(), "v")
	tag, err := client.LatestRelease(ctx, "owner", "repo")
	if err != nil {
		t.Fatalf("LatestRelease() unexpected error = %v", err)
	}
	if tag != "v1.5.0" {
		t.Errorf("LatestRelease() = %q, want v1.5.0", tag)
	}
}

// TestLatestRelease_WithTagPrefix_NoMatch asserts that ErrReleaseNotFound is
// returned when no release matches the given prefix.
func TestLatestRelease_WithTagPrefix_NoMatch(t *testing.T) {
	server := newMultiPrefixServer(t)
	defer server.Close()

	client := NewClient("")
	client.baseURL = server.URL

	ctx := WithTagPrefix(context.Background(), "server/")
	_, err := client.LatestRelease(ctx, "owner", "repo")
	if err == nil {
		t.Fatal("LatestRelease() expected error, got nil")
	}
	if !errors.Is(err, ErrReleaseNotFound) {
		t.Errorf("LatestRelease() error = %v, want ErrReleaseNotFound", err)
	}
}

// TestLatestRelease_WithTagPrefix_Pagination asserts that LatestRelease scans
// beyond the first page when the matching release is not on page 1.
func TestLatestRelease_WithTagPrefix_Pagination(t *testing.T) {
	// Page 1 has only CLI releases; the lib release is on page 2.
	page1 := `[{"tag_name": "cli/v3.0.0"}, {"tag_name": "cli/v2.5.0"}]`
	page2 := `[{"tag_name": "v1.9.0"}, {"tag_name": "cli/v1.0.0"}]`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/owner/repo/releases" {
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
		switch r.URL.Query().Get("page") {
		case "1", "":
			_, _ = w.Write([]byte(page1))
		case "2":
			_, _ = w.Write([]byte(page2))
		default:
			_, _ = w.Write([]byte(`[]`))
		}
	}))
	defer server.Close()

	client := NewClient("")
	client.baseURL = server.URL

	ctx := WithTagPrefix(context.Background(), "v")
	tag, err := client.LatestRelease(ctx, "owner", "repo")
	if err != nil {
		t.Fatalf("LatestRelease() unexpected error = %v", err)
	}
	if tag != "v1.9.0" {
		t.Errorf("LatestRelease() = %q, want v1.9.0", tag)
	}
}

// TestLatestRelease_WithTagPrefix_EmptyPrefix asserts that an empty prefix
// behaves the same as no filter (uses /releases/latest directly).
func TestLatestRelease_WithTagPrefix_EmptyPrefix(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/owner/repo/releases/latest" {
			t.Errorf("expected /releases/latest path, got %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"tag_name": "v2.0.0"}`))
	}))
	defer server.Close()

	client := NewClient("")
	client.baseURL = server.URL

	ctx := WithTagPrefix(context.Background(), "")
	tag, err := client.LatestRelease(ctx, "owner", "repo")
	if err != nil {
		t.Fatalf("LatestRelease() unexpected error = %v", err)
	}
	if tag != "v2.0.0" {
		t.Errorf("LatestRelease() = %q, want v2.0.0", tag)
	}
}

// newMultiPrefixServer returns a test server that simulates a repo with two
// release lines: plain "v*" (library) and "cli/v*" (CLI tool), interleaved by
// publish date as GitHub would return them from /releases.
//
// Release order (newest first): cli/v2.0.0, v1.5.0, cli/v1.3.0, v1.2.0
func newMultiPrefixServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/owner/repo/releases" {
			t.Errorf("unexpected path %s; expected /repos/owner/repo/releases", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
		// Only page 1 has data; subsequent pages return empty to signal end of results.
		if r.URL.Query().Get("page") != "" && r.URL.Query().Get("page") != "1" {
			_, _ = w.Write([]byte(`[]`))
			return
		}
		_, _ = w.Write([]byte(`[
			{"tag_name": "cli/v2.0.0"},
			{"tag_name": "v1.5.0"},
			{"tag_name": "cli/v1.3.0"},
			{"tag_name": "v1.2.0"}
		]`))
	}))
}
