// Package sigstore verifies checksum manifests signed keylessly with Sigstore
// (for example by `cosign sign-blob` in a GitHub Actions workflow). It
// implements ghrelease.SignatureVerifier and lives in its own module so the
// core ghreleases package stays dependency-free.
//
// The signer's identity must be pinned: a valid Sigstore signature from an
// arbitrary identity proves nothing, so NewVerifier rejects an Identity that
// does not name both the certificate subject and the OIDC issuer, and a
// SubjectRegexp that is unanchored or matches unrelated identities.
// WithInsecureSubjectPattern is the explicit escape hatch for the latter.
package sigstore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"regexp/syntax"

	"github.com/sigstore/sigstore-go/pkg/bundle"
	"github.com/sigstore/sigstore-go/pkg/root"
	"github.com/sigstore/sigstore-go/pkg/verify"
)

// BundleSuffix is the default suffix appended to the manifest's asset name to
// locate its Sigstore bundle, matching
// `cosign sign-blob --bundle <manifest>.sigstore.json`. See WithBundleSuffix.
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

// WithBundleSuffix overrides BundleSuffix for releases that name their bundle
// differently (for example "<manifest>.bundle").
func WithBundleSuffix(suffix string) Option {
	return func(v *Verifier) { v.bundleSuffix = suffix }
}

// WithInsecureSubjectPattern is an escape hatch: it lets NewVerifier accept a
// SubjectRegexp that is unanchored or matches any signer. A valid signature from
// an arbitrary identity proves nothing about authenticity, so only use it when
// you deliberately want that.
func WithInsecureSubjectPattern() Option {
	return func(v *Verifier) { v.insecureSubject = true }
}

// Verifier authenticates a manifest against a Sigstore bundle and a pinned
// signer Identity. It implements ghrelease.SignatureVerifier.
type Verifier struct {
	id              Identity
	trustedRootJSON []byte
	bundleSuffix    string
	insecureSubject bool
}

// NewVerifier validates the pinned identity and returns a Verifier.
func NewVerifier(id Identity, opts ...Option) (*Verifier, error) {
	if id.SubjectRegexp == "" || id.Issuer == "" {
		return nil, errors.New("sigstore: Identity must pin both SubjectRegexp and Issuer")
	}
	re, err := regexp.Compile(id.SubjectRegexp)
	if err != nil {
		return nil, fmt.Errorf("sigstore: invalid SubjectRegexp: %w", err)
	}
	v := &Verifier{id: id, bundleSuffix: BundleSuffix}
	for _, opt := range opts {
		opt(v)
	}
	if !v.insecureSubject {
		if err := checkSubjectPattern(id.SubjectRegexp, re); err != nil {
			return nil, err
		}
	}
	if v.bundleSuffix == "" {
		// An empty suffix would point the bundle lookup at the manifest itself.
		return nil, errors.New("sigstore: bundle suffix must not be empty")
	}
	return v, nil
}

// permissiveProbes are subjects no legitimate pin matches: the empty string and
// identities from an unrelated host. A pattern that matches any of them accepts
// signers it was never meant to.
var permissiveProbes = []string{
	"",
	"a",
	"https://example.invalid/evil/.github/workflows/x.yml@refs/heads/main",
}

// checkSubjectPattern rejects SubjectRegexp values that would accept signers
// other than the intended workflow: patterns not anchored at both ends, and
// patterns that match unrelated identities. WithInsecureSubjectPattern opts out.
func checkSubjectPattern(pattern string, re *regexp.Regexp) error {
	if !anchoredAtBothEnds(pattern) {
		return fmt.Errorf("sigstore: SubjectRegexp %q must be anchored with ^ and $ around the whole pattern, e.g. ^(?:a|b)$ for alternatives (or use WithInsecureSubjectPattern)", pattern)
	}
	for _, probe := range permissiveProbes {
		if re.MatchString(probe) {
			return fmt.Errorf("sigstore: SubjectRegexp %q matches unrelated identities such as %q (or use WithInsecureSubjectPattern)", pattern, probe)
		}
	}
	return nil
}

// anchoredAtBothEnds reports whether every match of pattern must span the whole
// subject: the parsed pattern is a sequence that begins with ^ and ends with $.
// Reading the parse tree, not the text, rejects a top-level alternation such as
// "^a|b$" (whose branches are each anchored at only one end) and an escaped "\$".
func anchoredAtBothEnds(pattern string) bool {
	re, err := syntax.Parse(pattern, syntax.Perl)
	if err != nil || re.Op != syntax.OpConcat || len(re.Sub) < 2 {
		return false
	}
	return re.Sub[0].Op == syntax.OpBeginText && re.Sub[len(re.Sub)-1].Op == syntax.OpEndText
}

// SignatureName implements ghrelease.SignatureVerifier.
func (v *Verifier) SignatureName(manifestName string) string {
	return manifestName + v.bundleSuffix
}

func (v *Verifier) trustedRoot() (root.TrustedMaterial, error) {
	if len(v.trustedRootJSON) > 0 {
		return root.NewTrustedRootFromJSON(v.trustedRootJSON)
	}
	return root.FetchTrustedRoot()
}

// Verify implements ghrelease.SignatureVerifier. It checks the bundle's
// signature over content (read to EOF), the Fulcio certificate chain, the
// transparency log entry and signed certificate timestamp, and that the
// certificate identity matches the pinned Identity. The same verifier
// authenticates a checksum manifest or a downloaded asset.
func (v *Verifier) Verify(_ context.Context, content io.Reader, bundleJSON []byte) error {
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
	policy := verify.NewPolicy(verify.WithArtifact(content), verify.WithCertificateIdentity(certID))
	if _, err := sv.Verify(&b, policy); err != nil {
		return fmt.Errorf("sigstore: verify: %w", err)
	}
	return nil
}
