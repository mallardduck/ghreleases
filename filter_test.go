package ghrelease

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func makeRelease(tag string, published time.Time, prerelease, draft bool) *Release {
	return &Release{
		TagName:     tag,
		Name:        tag,
		PublishedAt: published,
		Prerelease:  prerelease,
		Draft:       draft,
	}
}

var (
	jan1  = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	jan15 = time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	feb1  = time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	feb28 = time.Date(2026, 2, 28, 0, 0, 0, 0, time.UTC)
	mar1  = time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
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

func TestExcludePrereleases(t *testing.T) {
	releases := []*Release{
		makeRelease("v1.0.0", jan1, false, false),
		makeRelease("v1.1.0-beta.1", jan15, true, false),
		makeRelease("v2.0.0", feb1, false, false),
		makeRelease("v2.1.0-rc.1", feb28, true, false),
	}

	got := ExcludePrereleases(releases)
	if len(got) != 2 {
		t.Fatalf("ExcludePrereleases() len = %d, want 2", len(got))
	}
	if got[0].TagName != "v1.0.0" || got[1].TagName != "v2.0.0" {
		t.Errorf("ExcludePrereleases() tags = %v, %v, want v1.0.0, v2.0.0", got[0].TagName, got[1].TagName)
	}
}

func TestExcludePrereleases_empty(t *testing.T) {
	got := ExcludePrereleases(nil)
	if len(got) != 0 {
		t.Errorf("ExcludePrereleases(nil) len = %d, want 0", len(got))
	}
}

func TestExcludeDrafts(t *testing.T) {
	releases := []*Release{
		makeRelease("v1.0.0", jan1, false, false),
		makeRelease("v1.1.0", jan15, false, true),
		makeRelease("v2.0.0", feb1, false, false),
	}

	got := ExcludeDrafts(releases)
	if len(got) != 2 {
		t.Fatalf("ExcludeDrafts() len = %d, want 2", len(got))
	}
	if got[0].TagName != "v1.0.0" || got[1].TagName != "v2.0.0" {
		t.Errorf("ExcludeDrafts() got unexpected tags")
	}
}

func TestFilterByDateRange(t *testing.T) {
	releases := []*Release{
		makeRelease("v1.0.0", jan1, false, false),
		makeRelease("v1.1.0", jan15, false, false),
		makeRelease("v2.0.0", feb1, false, false),
		makeRelease("v2.1.0", feb28, false, false),
		makeRelease("v3.0.0", mar1, false, false),
	}

	tests := []struct {
		name     string
		from, to time.Time
		wantTags []string
	}{
		{
			name:     "exact month of February",
			from:     time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
			to:       time.Date(2026, 2, 28, 23, 59, 59, 0, time.UTC),
			wantTags: []string{"v2.0.0", "v2.1.0"},
		},
		{
			name:     "no lower bound",
			to:       jan15,
			wantTags: []string{"v1.0.0", "v1.1.0"},
		},
		{
			name:     "no upper bound",
			from:     feb28,
			wantTags: []string{"v2.1.0", "v3.0.0"},
		},
		{
			name:     "no bounds returns all",
			wantTags: []string{"v1.0.0", "v1.1.0", "v2.0.0", "v2.1.0", "v3.0.0"},
		},
		{
			name:     "range with no matches",
			from:     time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
			to:       time.Date(2027, 12, 31, 0, 0, 0, 0, time.UTC),
			wantTags: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FilterByDateRange(releases, tt.from, tt.to)
			if len(got) != len(tt.wantTags) {
				t.Fatalf("FilterByDateRange() len = %d, want %d", len(got), len(tt.wantTags))
			}
			for i, r := range got {
				if r.TagName != tt.wantTags[i] {
					t.Errorf("FilterByDateRange()[%d] = %s, want %s", i, r.TagName, tt.wantTags[i])
				}
			}
		})
	}
}

func TestFilterByDateRange_zeroPublishedAt(t *testing.T) {
	releases := []*Release{
		{TagName: "v1.0.0"},
		makeRelease("v2.0.0", feb1, false, false),
	}
	got := FilterByDateRange(releases, jan1, mar1)
	if len(got) != 1 || got[0].TagName != "v2.0.0" {
		t.Errorf("FilterByDateRange() should exclude zero PublishedAt, got %v", got)
	}
}

func TestLatestPerGroup(t *testing.T) {
	releases := []*Release{
		makeRelease("v1.2.0", feb1, false, false),
		makeRelease("v1.1.0", jan15, false, false),
		makeRelease("v2.0.0", jan1, false, false),
		makeRelease("v1.3.0", mar1, false, false),
	}

	got := LatestPerGroup(releases, GroupByMajorVersion)
	if len(got) != 2 {
		t.Fatalf("LatestPerGroup() len = %d, want 2", len(got))
	}
	// v1 group: v1.3.0 is newest; v2 group: v2.0.0
	if got[0].TagName != "v1.3.0" {
		t.Errorf("LatestPerGroup()[0] = %s, want v1.3.0", got[0].TagName)
	}
	if got[1].TagName != "v2.0.0" {
		t.Errorf("LatestPerGroup()[1] = %s, want v2.0.0", got[1].TagName)
	}
}

func TestLatestPerGroup_emptyKey(t *testing.T) {
	releases := []*Release{
		makeRelease("v1.0.0", jan1, false, false),
	}
	// groupFn always returns empty → all skipped
	got := LatestPerGroup(releases, func(_ *Release) string { return "" })
	if len(got) != 0 {
		t.Errorf("LatestPerGroup() with empty key len = %d, want 0", len(got))
	}
}

func TestFilterByTagPrefix(t *testing.T) {
	releases := []*Release{
		makeRelease("v1.0.0", jan1, false, false),
		makeRelease("sub-pkg/v2.0.0", feb1, false, false),
		makeRelease("sub-pkg/v1.0.0", jan15, false, false),
		makeRelease("other/v1.0.0", mar1, false, false),
	}

	tests := []struct {
		prefix   string
		wantTags []string
	}{
		{"sub-pkg/", []string{"sub-pkg/v2.0.0", "sub-pkg/v1.0.0"}},
		{"v", []string{"v1.0.0"}},
		{"", []string{"v1.0.0", "sub-pkg/v2.0.0", "sub-pkg/v1.0.0", "other/v1.0.0"}}, // empty = all
		{"no-match/", []string{}},
	}

	for _, tt := range tests {
		t.Run("prefix="+tt.prefix, func(t *testing.T) {
			got := FilterByTagPrefix(releases, tt.prefix)
			if len(got) != len(tt.wantTags) {
				t.Fatalf("FilterByTagPrefix(%q) len = %d, want %d", tt.prefix, len(got), len(tt.wantTags))
			}
			for i, r := range got {
				if r.TagName != tt.wantTags[i] {
					t.Errorf("FilterByTagPrefix(%q)[%d] = %q, want %q", tt.prefix, i, r.TagName, tt.wantTags[i])
				}
			}
		})
	}
}

func TestGroupByMajorVersion(t *testing.T) {
	tests := []struct {
		tag  string
		want string
	}{
		// Plain semver
		{"v1.2.3", "v1"},
		{"v2.0.0", "v2"},
		{"v1.2.3-beta.1", "v1"},
		{"1.2.3", "1"},
		{"v10.5.2", "v10"},
		// Non-semver → full tag
		{"vmain", "vmain"},
		{"main", "main"},
		// Non-numeric minor doesn't affect major grouping
		{"v1.x.1", "v1"},
		// Monorepo path prefixes — path preserved in group key
		{"sub-pkg/v1.2.3", "sub-pkg/v1"},
		{"sub-pkg/v1.4.0", "sub-pkg/v1"},     // same group as above
		{"top-level/v1.0.0", "top-level/v1"}, // different group from sub-pkg
		{"sub-pkg/notver", "sub-pkg/notver"}, // fallback: non-semver after prefix
	}

	for _, tt := range tests {
		t.Run(tt.tag, func(t *testing.T) {
			r := &Release{TagName: tt.tag}
			got := GroupByMajorVersion(r)
			if got != tt.want {
				t.Errorf("GroupByMajorVersion(%q) = %q, want %q", tt.tag, got, tt.want)
			}
		})
	}
}

func TestGroupByMinorVersion(t *testing.T) {
	tests := []struct {
		tag  string
		want string
	}{
		// Plain semver
		{"v1.2.3", "v1.2"},
		{"v1.2.3-beta.1", "v1.2"},
		{"v2.0.0", "v2.0"},
		{"1.2.3", "1.2"},
		// Non-semver → full tag
		{"v1.x.1", "v1.x.1"},
		{"vmain", "vmain"},
		{"v1", "v1"}, // not enough components → full tag
		// Monorepo path prefixes
		{"sub-pkg/v1.2.3", "sub-pkg/v1.2"},
		{"sub-pkg/v1.2.9", "sub-pkg/v1.2"}, // same group as above
		{"sub-pkg/v1.3.0", "sub-pkg/v1.3"}, // different minor
	}

	for _, tt := range tests {
		t.Run(tt.tag, func(t *testing.T) {
			r := &Release{TagName: tt.tag}
			got := GroupByMinorVersion(r)
			if got != tt.want {
				t.Errorf("GroupByMinorVersion(%q) = %q, want %q", tt.tag, got, tt.want)
			}
		})
	}
}
