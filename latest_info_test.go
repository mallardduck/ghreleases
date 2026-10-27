package ghrelease

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

const latestInfoPage = `[
	{"id":1,"tag_name":"v1.1.0-rc.2","name":"rc2","body":"rc notes","draft":false,"prerelease":true,"published_at":"2026-03-01T00:00:00Z","assets":[{"name":"a.tgz","url":"https://example.invalid/a","size":3}]},
	{"id":2,"tag_name":"v1.0.5","name":"five","body":"stable notes","draft":false,"prerelease":false,"published_at":"2026-02-10T00:00:00Z","assets":[]},
	{"id":3,"tag_name":"cli/v9.0.0","name":"cli","body":"cli notes","draft":false,"prerelease":false,"published_at":"2026-02-01T00:00:00Z","assets":[]}
]`

func latestInfoClient(t *testing.T) *Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/owner/repo/releases/latest":
			_, _ = w.Write([]byte(`{"id":9,"tag_name":"v1.0.5","name":"five","body":"stable notes","draft":false,"prerelease":false,"published_at":"2026-02-10T00:00:00Z","assets":[]}`))
		case r.URL.Path == "/repos/owner/repo/releases/tags/v1.0.5":
			_, _ = w.Write([]byte(`{"id":2,"tag_name":"v1.0.5","name":"five","body":"stable notes","draft":false,"prerelease":false,"published_at":"2026-02-10T00:00:00Z","assets":[]}`))
		case r.URL.Path == "/repos/owner/repo/releases" && r.URL.Query().Get("page") == "1":
			_, _ = w.Write([]byte(latestInfoPage))
		case r.URL.Path == "/repos/owner/repo/releases":
			_, _ = w.Write([]byte(`[]`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	c := NewClient("")
	c.baseURL = server.URL
	return c
}

func TestLatestReleaseInfo(t *testing.T) {
	tests := []struct {
		name           string
		ctx            context.Context
		wantTag        string
		wantBody       string
		wantPrerelease bool
	}{
		{"default uses the latest endpoint", context.Background(), "v1.0.5", "stable notes", false},
		{"tag prefix skips prereleases", WithTagPrefix(context.Background(), "v"), "v1.0.5", "stable notes", false},
		{"include prereleases returns the highest, flagged", WithPrereleases(WithTagPrefix(context.Background(), "v"), true), "v1.1.0-rc.2", "rc notes", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rel, err := latestInfoClient(t).LatestReleaseInfo(tt.ctx, "owner", "repo")
			if err != nil {
				t.Fatalf("LatestReleaseInfo() error = %v", err)
			}
			if rel.TagName != tt.wantTag {
				t.Errorf("TagName = %q, want %q", rel.TagName, tt.wantTag)
			}
			if rel.Body != tt.wantBody {
				t.Errorf("Body = %q, want %q", rel.Body, tt.wantBody)
			}
			if rel.Prerelease != tt.wantPrerelease {
				t.Errorf("Prerelease = %v, want %v", rel.Prerelease, tt.wantPrerelease)
			}
			if rel.PublishedAt.IsZero() {
				t.Error("PublishedAt is zero")
			}
		})
	}
}

func TestLatestReleaseInfo_IncludesAssets(t *testing.T) {
	ctx := WithPrereleases(WithTagPrefix(context.Background(), "v"), true)
	rel, err := latestInfoClient(t).LatestReleaseInfo(ctx, "owner", "repo")
	if err != nil {
		t.Fatalf("LatestReleaseInfo() error = %v", err)
	}
	if len(rel.Assets) != 1 || rel.Assets[0].Name != "a.tgz" {
		t.Errorf("Assets = %+v, want one asset a.tgz", rel.Assets)
	}
}

func TestLatestReleaseInfo_NoMatch(t *testing.T) {
	_, err := latestInfoClient(t).LatestReleaseInfo(WithTagPrefix(context.Background(), "nope/"), "owner", "repo")
	if !errors.Is(err, ErrReleaseNotFound) {
		t.Errorf("error = %v, want ErrReleaseNotFound", err)
	}
}

func TestGetRelease_PopulatesBody(t *testing.T) {
	rel, err := latestInfoClient(t).GetRelease(context.Background(), "owner", "repo", "v1.0.5")
	if err != nil {
		t.Fatalf("GetRelease() error = %v", err)
	}
	if rel.Body != "stable notes" {
		t.Errorf("Body = %q, want %q", rel.Body, "stable notes")
	}
}
