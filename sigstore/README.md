# ghreleases/sigstore

Keyless [Sigstore](https://sigstore.dev) verification of checksum manifests for
`ghreleases`. Separate module so the core package stays dependency-free.

```go
v, err := sigstore.NewVerifier(sigstore.Identity{
	SubjectRegexp: `^https://github\.com/OWNER/REPO/\.github/workflows/release\.yml@refs/tags/v.+$`,
	Issuer:        "https://token.actions.githubusercontent.com",
})
client := ghrelease.NewClient(token, ghrelease.WithManifestVerifier(v))
sums, err := client.FetchChecksums(ctx, checksumsURL) // also fetches checksumsURL + ".sigstore.json"
```

The release must publish `<manifest>.sigstore.json`, e.g. via
`cosign sign-blob --yes --bundle <manifest>.sigstore.json <manifest>`.
A missing or invalid bundle fails the fetch (`ErrSignatureMissing` / `ErrSignatureInvalid`).
The public-good trusted root is fetched over TUF unless `WithTrustedRoot` is given.

With GitHub API asset URLs (`.../releases/assets/<id>`) the bundle cannot be found by name; look it up in
`Release.Assets` via `v.SignatureName(manifestName)` and pass `ghrelease.WithSignatureURL(ctx, bundleURL)`.
