# Changelog

## Unreleased

### Added (additive APIs)

- `WithPrereleases(ctx, true)` makes `LatestRelease` include prereleases and return the highest
  semver tag instead of the most recently published one. Drafts stay excluded. Composes with
  `WithTagPrefix`. Default behavior is unchanged.
- `CompareVersions(a, b)` compares release tags by semver precedence (main version first, then
  prerelease identifiers per the semver spec).
- `ReleaseFilter.IncludePrereleases`.
- `ManifestVerifier` and `WithManifestVerifier`: authenticate checksum manifests against a detached
  signature before they are parsed (`FetchChecksums`, `FetchChecksumsLenient`, `FetchMultiHashChecksums`).
  Fails closed with `ErrSignatureMissing` / `ErrSignatureInvalid`. Off by default. `WithSignatureURL(ctx, url)`
  supplies the exact signature URL, needed for GitHub API asset URLs (opaque IDs).
- `sigstore` module: keyless Sigstore bundle verifier with a pinned certificate identity.

### Behaviour changes

- `WithTagPrefix` now preserves other filter settings already on the context instead of replacing them.

## v0.7.0

### Added (additive APIs)

- `Option`, `WithBaseURL`, `WithAllowedHosts`; `NewClient` and `NewClientWithHTTP` take variadic options.
- `ErrHostNotAllowed`, `ErrChecksumFormat`, and `*HTTPStatusError`.
- `TemplateVars.Extra` for caller-supplied template variables.
- `ParseChecksumFileLenient`, `(*Client).FetchChecksumsLenient`.
- `ParseHashOrder`, `ParseMultiHashChecksumFile`, `(*Client).FetchMultiHashChecksums`.
- `errors.Is(err, ErrReleaseNotFound)` / `ErrRefNotFound` now coexist with `errors.As(err, *HTTPStatusError)`.

### Behaviour changes

- The `Authorization` token is no longer sent to non-GitHub hosts (only the API base host,
  `github.com`, `*.github.com`, `githubusercontent.com`, `*.githubusercontent.com`), and is
  dropped on redirects to other hosts.
- `ParseSource` is stricter: URLs are anchored (`https://notgithub.com/a/b` is rejected) and
  shorthand must be exactly `owner/repo` (`a/b/c` is rejected). SSH repo names may no longer contain `/`.
- The default HTTP client no longer has a 30s total timeout. API and checksum calls apply
  a 30s timeout only when the context has no deadline; `Download` is bounded by its context.
- HTTP status errors now read `unexpected status code: N (URL)`; `Download` returns
  `*HTTPStatusError` instead of `download failed: status N`.
