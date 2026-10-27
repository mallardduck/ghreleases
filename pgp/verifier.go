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
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/ProtonMail/go-crypto/openpgp"
)

// SignatureSuffix is appended to the manifest name to locate its detached
// signature, matching `gpg --armor --detach-sign <manifest>`.
const SignatureSuffix = ".asc"

const (
	armoredKeyHeader = "-----BEGIN PGP PUBLIC KEY BLOCK-----"
	armoredKeyFooter = "-----END PGP PUBLIC KEY BLOCK-----"
	armoredSigHeader = "-----BEGIN PGP SIGNATURE-----"
)

// Option configures a Verifier.
type Option func(*Verifier)

// WithFingerprints narrows trust from "any key in the keyring" to signatures
// made by keys whose primary-key fingerprint is listed (40 hex characters,
// case and spaces ignored). Every listed fingerprint must be present in the
// keyring, otherwise NewVerifier fails: a pin that can never match would
// otherwise only surface later as every signature being rejected.
func WithFingerprints(fingerprints ...string) Option {
	return func(v *Verifier) {
		v.pins = fingerprints
		v.pinned = true
	}
}

// WithSignatureSuffix overrides SignatureSuffix for releases that name their
// signatures differently (for example ".sig").
func WithSignatureSuffix(suffix string) Option {
	return func(v *Verifier) { v.suffix = suffix }
}

// Verifier authenticates a manifest against a detached OpenPGP signature made
// by a key in its keyring. It implements ghrelease.ManifestVerifier.
type Verifier struct {
	keyring openpgp.EntityList
	suffix  string
	pins    []string
	pinned  bool
}

// NewVerifier parses keyring (one or more concatenated armored or binary public
// key blocks, such as a project's KEYS file) and returns a Verifier. It fails
// if the keyring holds no keys, or if a WithFingerprints pin is malformed or
// names a key that is not in the keyring.
func NewVerifier(keyring []byte, opts ...Option) (*Verifier, error) {
	v := &Verifier{suffix: SignatureSuffix}
	for _, opt := range opts {
		opt(v)
	}
	if v.suffix == "" {
		return nil, errors.New("pgp: signature suffix must not be empty")
	}
	entities, err := parseKeyring(keyring)
	if err != nil {
		return nil, err
	}
	if v.pinned {
		if entities, err = pinEntities(entities, v.pins); err != nil {
			return nil, err
		}
	}
	v.keyring = entities
	return v, nil
}

func parseKeyring(keyring []byte) (openpgp.EntityList, error) {
	if len(bytes.TrimSpace(keyring)) == 0 {
		return nil, errors.New("pgp: keyring is empty")
	}
	var all openpgp.EntityList
	if bytes.Contains(keyring, []byte(armoredKeyHeader)) {
		// ReadArmoredKeyRing stops after the first block; KEYS files hold many.
		rest := keyring
		for {
			i := bytes.Index(rest, []byte(armoredKeyHeader))
			if i < 0 {
				break
			}
			rest = rest[i:]
			end := bytes.Index(rest, []byte(armoredKeyFooter))
			if end < 0 {
				return nil, errors.New("pgp: unterminated armored key block")
			}
			end += len(armoredKeyFooter)
			list, err := openpgp.ReadArmoredKeyRing(bytes.NewReader(rest[:end]))
			if err != nil {
				return nil, fmt.Errorf("pgp: read keyring: %w", err)
			}
			all = append(all, list...)
			rest = rest[end:]
		}
	} else {
		list, err := openpgp.ReadKeyRing(bytes.NewReader(keyring))
		if err != nil {
			return nil, fmt.Errorf("pgp: read keyring: %w", err)
		}
		all = list
	}
	if len(all) == 0 {
		return nil, errors.New("pgp: keyring contains no keys")
	}
	return all, nil
}

func normalizeFingerprint(fp string) (string, error) {
	fp = strings.ToUpper(strings.Join(strings.Fields(fp), ""))
	if b, err := hex.DecodeString(fp); err != nil || len(b) != 20 {
		return "", fmt.Errorf("pgp: fingerprint %q is not 40 hex characters", fp)
	}
	return fp, nil
}

func fingerprintOf(e *openpgp.Entity) string {
	return strings.ToUpper(hex.EncodeToString(e.PrimaryKey.Fingerprint))
}

func pinEntities(entities openpgp.EntityList, pins []string) (openpgp.EntityList, error) {
	if len(pins) == 0 {
		return nil, errors.New("pgp: WithFingerprints needs at least one fingerprint")
	}
	byFP := make(map[string]*openpgp.Entity, len(entities))
	for _, e := range entities {
		byFP[fingerprintOf(e)] = e
	}
	var out openpgp.EntityList
	for _, p := range pins {
		fp, err := normalizeFingerprint(p)
		if err != nil {
			return nil, err
		}
		e, ok := byFP[fp]
		if !ok {
			return nil, fmt.Errorf("pgp: pinned fingerprint %s is not in the keyring", fp)
		}
		out = append(out, e)
	}
	return out, nil
}

// SignatureName implements ghrelease.ManifestVerifier.
func (v *Verifier) SignatureName(manifestName string) string { return manifestName + v.suffix }

// Verify implements ghrelease.ManifestVerifier. It accepts an armored or binary
// detached signature and requires it to be a valid signature over exactly these
// manifest bytes by a key in the (optionally pinned) keyring.
func (v *Verifier) Verify(_ context.Context, manifest, signature []byte) error {
	if len(bytes.TrimSpace(signature)) == 0 {
		return errors.New("pgp: empty signature")
	}
	var err error
	if bytes.HasPrefix(bytes.TrimSpace(signature), []byte(armoredSigHeader)) {
		_, err = openpgp.CheckArmoredDetachedSignature(v.keyring, bytes.NewReader(manifest), bytes.NewReader(signature), nil)
	} else {
		_, err = openpgp.CheckDetachedSignature(v.keyring, bytes.NewReader(manifest), bytes.NewReader(signature), nil)
	}
	if err != nil {
		return fmt.Errorf("pgp: verify: %w", err)
	}
	return nil
}
