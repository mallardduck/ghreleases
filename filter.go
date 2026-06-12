package ghrelease

import "context"

type releaseFilterKey struct{}

// ReleaseFilter configures how LatestRelease selects a release in repositories
// that publish multiple release lines under different tag prefixes (e.g. "v*"
// for a library and "cli/v*" for a CLI tool, following Go multi-module
// conventions).
type ReleaseFilter struct {
	// TagPrefix restricts results to releases whose tag starts with this string.
	// An empty prefix disables filtering and preserves the default behavior.
	TagPrefix string
}

// WithTagPrefix returns a context that instructs LatestRelease to scan all
// releases and return the most recently published one whose tag starts with
// prefix. When prefix is non-empty, LatestRelease uses the paginated
// /releases endpoint instead of /releases/latest, because GitHub's
// /releases/latest does not support prefix filtering.
//
// Example — find the latest CLI release in a multi-prefix repo:
//
//	ctx := ghrelease.WithTagPrefix(context.Background(), "cli/")
//	tag, err := client.LatestRelease(ctx, "rancher", "ob-charts-tool")
func WithTagPrefix(ctx context.Context, prefix string) context.Context {
	return context.WithValue(ctx, releaseFilterKey{}, ReleaseFilter{TagPrefix: prefix})
}

func releaseFilterFromContext(ctx context.Context) (ReleaseFilter, bool) {
	f, ok := ctx.Value(releaseFilterKey{}).(ReleaseFilter)
	return f, ok
}
