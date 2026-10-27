package sigstore

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// realRelease is a genuine, untouched checksum manifest and Sigstore bundle
// taken from a public GitHub release. They differ in naming, signer repo and
// ref type, which is what the verifier has to cope with in the wild.
type realRelease struct {
	name     string
	dir      string
	manifest string // manifest asset name
	bundle   string // bundle asset name
	// subject pins the exact workflow identity found in the release's certificate.
	subject string
}

const ghIssuer = "https://token.actions.githubusercontent.com"

var realReleases = []realRelease{
	{
		name: "goreleaser v2.19.0", dir: "goreleaser",
		manifest: "checksums.txt", bundle: "checksums.txt.sigstore.json",
		subject: `^https://github\.com/goreleaser/goreleaser/\.github/workflows/release\.yml@refs/tags/v2\.19\.0$`,
	},
	{
		name: "nfpm v2.48.0", dir: "nfpm",
		manifest: "checksums.txt", bundle: "checksums.txt.sigstore.json",
		subject: `^https://github\.com/goreleaser/nfpm/\.github/workflows/release\.yml@refs/tags/v2\.48\.0$`,
	},
	{
		// Signed from a branch ref, not a tag: a "refs/tags/v.+" pin would
		// wrongly reject this genuine release.
		name: "syft v1.55.0", dir: "syft",
		manifest: "syft_1.55.0_checksums.txt", bundle: "syft_1.55.0_checksums.txt.sigstore.json",
		subject: `^https://github\.com/anchore/syft/\.github/workflows/release\.yaml@refs/heads/main$`,
	},
	{
		name: "flux2 v2.10.0", dir: "flux2",
		manifest: "flux_2.10.0_checksums.txt", bundle: "flux_2.10.0_checksums.txt.sigstore.json",
		subject: `^https://github\.com/fluxcd/flux2/\.github/workflows/release\.yaml@refs/tags/v2\.10\.0$`,
	},
	{
		// Signed by a shared workflow in a different repository than the one
		// that publishes the release.
		name: "gum v2.0.2", dir: "gum",
		manifest: "checksums.txt", bundle: "checksums.txt.sigstore.json",
		subject: `^https://github\.com/charmbracelet/meta/\.github/workflows/goreleaser\.yml@refs/heads/main$`,
	},
}

func (r realRelease) read(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "releases", r.dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func (r realRelease) verifier(t *testing.T, opts ...Option) *Verifier {
	t.Helper()
	opts = append([]Option{WithTrustedRoot(fixture(t, "trusted_root.json"))}, opts...)
	v, err := NewVerifier(Identity{SubjectRegexp: r.subject, Issuer: ghIssuer}, opts...)
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	return v
}

func TestVerifier_RealReleases(t *testing.T) {
	for _, r := range realReleases {
		t.Run(r.name, func(t *testing.T) {
			v := r.verifier(t)
			if got := v.SignatureName(r.manifest); got != r.bundle {
				t.Fatalf("SignatureName(%q) = %q, want the release's asset name %q", r.manifest, got, r.bundle)
			}
			if err := v.Verify(context.Background(), r.read(t, r.manifest), r.read(t, r.bundle)); err != nil {
				t.Fatalf("genuine release rejected: %v", err)
			}
		})
	}
}

func TestVerifier_RealReleases_Tampered(t *testing.T) {
	for _, r := range realReleases {
		t.Run(r.name, func(t *testing.T) {
			v := r.verifier(t)
			manifest := r.read(t, r.manifest)
			bundle := r.read(t, r.bundle)

			// An attacker who swaps a binary also rewrites its checksum line.
			swapped := append([]byte("0000000000000000000000000000000000000000000000000000000000000000  evil.tar.gz\n"), manifest...)
			if err := v.Verify(context.Background(), swapped, bundle); err == nil {
				t.Error("manifest with an injected line was accepted")
			}
			if err := v.Verify(context.Background(), manifest[:len(manifest)-1], bundle); err == nil {
				t.Error("truncated manifest was accepted")
			}
		})
	}
}

// A bundle is only valid for the manifest it was made for. Replaying release
// A's genuine bundle next to release B's manifest, or accepting a signer other
// than the pinned one, must fail even though every individual file is genuine.
func TestVerifier_RealReleases_CrossRelease(t *testing.T) {
	for _, signer := range realReleases {
		for _, other := range realReleases {
			if signer.dir == other.dir {
				continue
			}
			t.Run(signer.name+" pin vs "+other.name+" files", func(t *testing.T) {
				v := signer.verifier(t)
				ctx := context.Background()
				if err := v.Verify(ctx, other.read(t, other.manifest), other.read(t, other.bundle)); err == nil {
					t.Error("another project's genuine release was accepted under this identity pin")
				}
				if err := v.Verify(ctx, other.read(t, other.manifest), signer.read(t, signer.bundle)); err == nil {
					t.Error("bundle replayed against a different project's manifest was accepted")
				}
			})
		}
	}
}

// Pinning to release tags is the recommended shape, but it must actually be
// enforced: a pin that demands a tag ref rejects a genuine branch-signed release.
func TestVerifier_TagPinRejectsBranchSignedRelease(t *testing.T) {
	syft := realReleases[2]
	v, err := NewVerifier(Identity{
		SubjectRegexp: `^https://github\.com/anchore/syft/\.github/workflows/release\.yaml@refs/tags/v.+$`,
		Issuer:        ghIssuer,
	}, WithTrustedRoot(fixture(t, "trusted_root.json")))
	if err != nil {
		t.Fatal(err)
	}
	if err := v.Verify(context.Background(), syft.read(t, syft.manifest), syft.read(t, syft.bundle)); err == nil {
		t.Fatal("branch-signed release accepted under a refs/tags pin")
	}
}

// yq publishes its bundle as "<manifest>.bundle" rather than ".sigstore.json".
// The verifier must be able to follow that convention.
func TestVerifier_NonDefaultBundleSuffix_YQ(t *testing.T) {
	yq := realRelease{
		dir: "yq", manifest: "checksums", bundle: "checksums.bundle",
		subject: `^https://github\.com/mikefarah/yq/\.github/workflows/release\.yml@refs/tags/v4\.54\.1$`,
	}
	v := yq.verifier(t, WithBundleSuffix(".bundle"))
	if got := v.SignatureName(yq.manifest); got != yq.bundle {
		t.Fatalf("SignatureName(%q) = %q, want %q", yq.manifest, got, yq.bundle)
	}
	if err := v.Verify(context.Background(), yq.read(t, yq.manifest), yq.read(t, yq.bundle)); err != nil {
		t.Fatalf("genuine yq release rejected: %v", err)
	}

	// The default suffix must be unchanged for everyone else.
	if got := yq.verifier(t).SignatureName("checksums"); got != "checksums.sigstore.json" {
		t.Errorf("default SignatureName = %q", got)
	}
}

// An identity that matches any signer proves nothing, however valid the
// signature is, so NewVerifier must refuse it just as it refuses an empty one.
func TestNewVerifier_RejectsUnpinnedIdentity(t *testing.T) {
	root := WithTrustedRoot(fixture(t, "trusted_root.json"))
	for _, re := range []string{
		`.*`,
		`.+`,
		`^.*$`,
		`github\.com/goreleaser/goreleaser`, // unanchored: also matches evil.example/github.com/goreleaser/goreleaser
		`^https://github\.com/goreleaser/goreleaser`, // anchored at the start only: matches ".../goreleaser-evil/..."
		`^https://github\.com/goreleaser/goreleaser\$`, // escaped dollar is a literal, not an end anchor
	} {
		if _, err := NewVerifier(Identity{SubjectRegexp: re, Issuer: ghIssuer}, root); err == nil {
			t.Errorf("NewVerifier accepted permissive SubjectRegexp %q", re)
		}
	}
}

// The permissive-pattern guard is a safe default, not a wall: a caller who
// deliberately wants to accept any signer from a trusted issuer can say so.
// The issuer pin and a valid pattern are still required.
func TestNewVerifier_InsecureSubjectPatternEscapeHatch(t *testing.T) {
	root := WithTrustedRoot(fixture(t, "trusted_root.json"))
	gr := realReleases[0]

	for _, re := range []string{`.*`, `.+`, `github\.com/goreleaser/goreleaser`} {
		v, err := NewVerifier(Identity{SubjectRegexp: re, Issuer: ghIssuer}, root, WithInsecureSubjectPattern())
		if err != nil {
			t.Fatalf("escape hatch did not allow %q: %v", re, err)
		}
		if err := v.Verify(context.Background(), gr.read(t, gr.manifest), gr.read(t, gr.bundle)); err != nil {
			t.Errorf("genuine release rejected under %q: %v", re, err)
		}
	}

	// The hatch relaxes the subject-shape check only.
	if _, err := NewVerifier(Identity{SubjectRegexp: `.*`}, root, WithInsecureSubjectPattern()); err == nil {
		t.Error("escape hatch must not waive the issuer pin")
	}
	if _, err := NewVerifier(Identity{SubjectRegexp: `(`, Issuer: ghIssuer}, root, WithInsecureSubjectPattern()); err == nil {
		t.Error("escape hatch must not accept an invalid regexp")
	}
	v, err := NewVerifier(Identity{SubjectRegexp: `.*`, Issuer: "https://accounts.google.com"}, root, WithInsecureSubjectPattern())
	if err != nil {
		t.Fatal(err)
	}
	if err := v.Verify(context.Background(), gr.read(t, gr.manifest), gr.read(t, gr.bundle)); err == nil {
		t.Error("escape hatch must not waive the issuer check at verification time")
	}
}

// An empty suffix would make the bundle URL equal the manifest URL.
func TestNewVerifier_RejectsEmptyBundleSuffix(t *testing.T) {
	_, err := NewVerifier(Identity{SubjectRegexp: testSAN, Issuer: testIssuer},
		WithTrustedRoot(fixture(t, "trusted_root.json")), WithBundleSuffix(""))
	if err == nil {
		t.Fatal("empty bundle suffix accepted")
	}
}
