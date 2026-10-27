// Package sigstore verifies checksum manifests signed keylessly with Sigstore
// (for example by `cosign sign-blob` in a GitHub Actions workflow). It
// implements ghrelease.ManifestVerifier and lives in its own module so the
// core ghreleases package stays dependency-free.
//
// The signer's identity must be pinned: a valid Sigstore signature from an
// arbitrary identity proves nothing, so NewVerifier rejects an Identity that
// does not name both the certificate subject and the OIDC issuer.
package sigstore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"regexp"

	"github.com/sigstore/sigstore-go/pkg/bundle"
	"github.com/sigstore/sigstore-go/pkg/root"
	"github.com/sigstore/sigstore-go/pkg/verify"
)

// BundleSuffix is appended to the manifest URL to locate its Sigstore bundle,
// matching `cosign sign-blob --bundle <manifest>.sigstore.json`.
const BundleSuffix = ".sigstore.json"

// Identity pins who may sign. For GitHub Actions keyless signing, SubjectRegexp
// matches the workflow identity (the certificate SAN, e.g.
// `^https://github\.com/OWNER/REPO/\.github/workflows/release\.yml@refs/tags/v.+$`)
// and Issuer is `https://token.actions.githubusercontent.com`.
type Identity struct {
	// SubjectRegexp must match the certificate's subject alternative name.
	SubjectRegexp string
	// Issuer is the exact OIDC issuer the certificate must carry.
	Issuer string
}

// Option configures a Verifier.
type Option func(*Verifier)

// WithTrustedRoot supplies a Sigstore trusted_root.json instead of fetching
// the public-good root over TUF. Use it for offline operation, tests, or a
// private Sigstore deployment.
func WithTrustedRoot(trustedRootJSON []byte) Option {
	return func(v *Verifier) { v.trustedRootJSON = trustedRootJSON }
}

// Verifier authenticates a manifest against a Sigstore bundle and a pinned
// signer Identity. It implements ghrelease.ManifestVerifier.
type Verifier struct {
	id              Identity
	trustedRootJSON []byte
}

// NewVerifier validates the pinned identity and returns a Verifier.
func NewVerifier(id Identity, opts ...Option) (*Verifier, error) {
	if id.SubjectRegexp == "" || id.Issuer == "" {
		return nil, errors.New("sigstore: Identity must pin both SubjectRegexp and Issuer")
	}
	if _, err := regexp.Compile(id.SubjectRegexp); err != nil {
		return nil, fmt.Errorf("sigstore: invalid SubjectRegexp: %w", err)
	}
	v := &Verifier{id: id}
	for _, opt := range opts {
		opt(v)
	}
	return v, nil
}

// SignatureName implements ghrelease.ManifestVerifier.
func (v *Verifier) SignatureName(manifestName string) string {
	return manifestName + BundleSuffix
}

func (v *Verifier) trustedRoot() (root.TrustedMaterial, error) {
	if len(v.trustedRootJSON) > 0 {
		return root.NewTrustedRootFromJSON(v.trustedRootJSON)
	}
	return root.FetchTrustedRoot()
}

// Verify implements ghrelease.ManifestVerifier. It checks the bundle's
// signature over manifest, the Fulcio certificate chain, the transparency log
// entry and signed certificate timestamp, and that the certificate identity
// matches the pinned Identity.
func (v *Verifier) Verify(_ context.Context, manifest, bundleJSON []byte) error {
	if len(bundleJSON) == 0 {
		return errors.New("sigstore: empty bundle")
	}
	var b bundle.Bundle
	if err := b.UnmarshalJSON(bundleJSON); err != nil {
		return fmt.Errorf("sigstore: parse bundle: %w", err)
	}
	trusted, err := v.trustedRoot()
	if err != nil {
		return fmt.Errorf("sigstore: trusted root: %w", err)
	}
	sv, err := verify.NewVerifier(trusted,
		verify.WithSignedCertificateTimestamps(1),
		verify.WithTransparencyLog(1),
		verify.WithObserverTimestamps(1),
	)
	if err != nil {
		return fmt.Errorf("sigstore: build verifier: %w", err)
	}
	certID, err := verify.NewShortCertificateIdentity(v.id.Issuer, "", "", v.id.SubjectRegexp)
	if err != nil {
		return fmt.Errorf("sigstore: certificate identity: %w", err)
	}
	policy := verify.NewPolicy(verify.WithArtifact(bytes.NewReader(manifest)), verify.WithCertificateIdentity(certID))
	if _, err := sv.Verify(&b, policy); err != nil {
		return fmt.Errorf("sigstore: verify: %w", err)
	}
	return nil
}
