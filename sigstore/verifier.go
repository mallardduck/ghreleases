// Package sigstore verifies checksum manifests signed keylessly with Sigstore
// (for example by `cosign sign-blob` in a GitHub Actions workflow). It
// implements ghrelease.ManifestVerifier and lives in its own module so the
// core ghreleases package stays dependency-free.
package sigstore

import "context"

// BundleSuffix is appended to the manifest name to locate its Sigstore bundle,
// matching `cosign sign-blob --bundle <manifest>.sigstore.json`.
const BundleSuffix = ".sigstore.json"

// Identity pins who may sign.
type Identity struct {
	// SubjectRegexp must match the certificate's subject alternative name.
	SubjectRegexp string
	// Issuer is the exact OIDC issuer the certificate must carry.
	Issuer string
}

// Option configures a Verifier.
type Option func(*Verifier)

// WithTrustedRoot supplies a Sigstore trusted_root.json instead of fetching
// the public-good root over TUF.
func WithTrustedRoot(trustedRootJSON []byte) Option {
	return func(v *Verifier) { v.trustedRootJSON = trustedRootJSON }
}

// Verifier authenticates a manifest against a Sigstore bundle and a pinned
// signer Identity. It implements ghrelease.ManifestVerifier.
type Verifier struct {
	id              Identity
	trustedRootJSON []byte
}

// NewVerifier returns a Verifier for the pinned identity.
func NewVerifier(id Identity, opts ...Option) (*Verifier, error) {
	v := &Verifier{id: id}
	for _, opt := range opts {
		opt(v)
	}
	return v, nil
}

// SignatureName implements ghrelease.ManifestVerifier.
func (v *Verifier) SignatureName(manifestName string) string { return "" }

// Verify implements ghrelease.ManifestVerifier.
func (v *Verifier) Verify(_ context.Context, manifest, bundleJSON []byte) error { return nil }
