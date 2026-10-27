// Package pgp verifies checksum manifests signed with detached OpenPGP
// signatures (for example Helm's "<file>.sha256sum.asc"). It implements
// ghrelease.ManifestVerifier and lives in its own module so the core
// ghreleases package stays dependency-free.
//
// Trust is explicit: NewVerifier requires a keyring and refuses to start
// without one, because a signature that is checked against no keys proves
// nothing.
package pgp

import (
	"context"
	"errors"
)

// SignatureSuffix is appended to the manifest name to locate its detached
// signature, matching `gpg --armor --detach-sign <manifest>`.
const SignatureSuffix = ".asc"

// Option configures a Verifier.
type Option func(*Verifier)

// WithFingerprints narrows trust from "any key in the keyring" to signatures
// made by keys whose primary-key fingerprint is listed (40 hex characters).
func WithFingerprints(fingerprints ...string) Option {
	return func(v *Verifier) {}
}

// WithSignatureSuffix overrides SignatureSuffix for releases that name their
// signatures differently (for example ".sig").
func WithSignatureSuffix(suffix string) Option {
	return func(v *Verifier) {}
}

// Verifier authenticates a manifest against a detached OpenPGP signature made
// by a key in its keyring. It implements ghrelease.ManifestVerifier.
type Verifier struct{}

// NewVerifier parses keyring (one or more concatenated armored or binary public
// key blocks) and returns a Verifier.
func NewVerifier(keyring []byte, opts ...Option) (*Verifier, error) {
	return nil, errors.New("pgp: not implemented")
}

// SignatureName implements ghrelease.ManifestVerifier.
func (v *Verifier) SignatureName(manifestName string) string { return "" }

// Verify implements ghrelease.ManifestVerifier.
func (v *Verifier) Verify(_ context.Context, manifest, signature []byte) error {
	return errors.New("pgp: not implemented")
}
