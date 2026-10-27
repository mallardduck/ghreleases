// Package integration exercises the real ghreleases client with the real pgp
// and sigstore verifiers against genuine signed release files. It is a separate,
// unpublished module because the core cannot import the verifier modules and the
// verifier modules should not depend on an unreleased core.
package integration

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	ghrelease "github.com/mallardduck/ghreleases"
	"github.com/mallardduck/ghreleases/pgp"
	"github.com/mallardduck/ghreleases/sigstore"
)

// Both verifiers must satisfy the core's single verifier interface.
var (
	_ ghrelease.SignatureVerifier = (*pgp.Verifier)(nil)
	_ ghrelease.SignatureVerifier = (*sigstore.Verifier)(nil)
)

// releaseServer serves the named files from dir at "/<name>"; names listed in
// override are served with that content instead (to simulate tampering).
func releaseServer(t *testing.T, dir string, override map[string][]byte) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		name := filepath.Base(r.URL.Path)
		if b, ok := override[name]; ok {
			_, _ = w.Write(b)
			return
		}
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(b)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func read(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

const (
	helmDir  = "../pgp/testdata/helm-v4.3.1"
	helmFile = "helm-v4.3.1-linux-amd64.tar.gz.sha256sum"

	grDir      = "../sigstore/testdata/releases/goreleaser"
	grManifest = "checksums.txt"
	grSubject  = `^https://github\.com/goreleaser/goreleaser/\.github/workflows/release\.yml@refs/tags/v2\.19\.0$`
	ghIssuer   = "https://token.actions.githubusercontent.com"
)

func helmVerifier(t *testing.T, opts ...pgp.Option) *pgp.Verifier {
	t.Helper()
	v, err := pgp.NewVerifier(read(t, helmDir+"/KEYS"), opts...)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func goreleaserVerifier(t *testing.T, subject string) *sigstore.Verifier {
	t.Helper()
	v, err := sigstore.NewVerifier(sigstore.Identity{SubjectRegexp: subject, Issuer: ghIssuer},
		sigstore.WithTrustedRoot(read(t, "../sigstore/testdata/trusted_root.json")))
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestPGP_AsManifestVerifier(t *testing.T) {
	good := releaseServer(t, helmDir, nil)
	c := ghrelease.NewClient("", ghrelease.WithManifestVerifier(helmVerifier(t)))

	sums, err := c.FetchChecksums(context.Background(), good.URL+"/"+helmFile)
	if err != nil {
		t.Fatalf("genuine helm manifest rejected: %v", err)
	}
	if got := sums["helm-v4.3.1-linux-amd64.tar.gz"]; len(got) != 64 {
		t.Errorf("checksum not parsed: %q", got)
	}

	tampered := releaseServer(t, helmDir, map[string][]byte{
		helmFile: bytes.Replace(read(t, helmDir+"/"+helmFile), []byte("0403a88a"), []byte("deadbeef"), 1),
	})
	sums, err = c.FetchChecksums(context.Background(), tampered.URL+"/"+helmFile)
	if !errors.Is(err, ghrelease.ErrSignatureInvalid) || sums != nil {
		t.Fatalf("tampered manifest: got (%v, %v), want (nil, ErrSignatureInvalid)", sums, err)
	}
}

func TestPGP_AsArtifactVerifier(t *testing.T) {
	srv := releaseServer(t, helmDir, nil)
	c := ghrelease.NewClient("", ghrelease.WithArtifactVerifier(helmVerifier(t)))
	want := read(t, helmDir+"/"+helmFile)

	got, res, err := c.DownloadToBytes(srv.URL+"/"+helmFile, ghrelease.DownloadOptions{})
	if err != nil || !bytes.Equal(got, want) || res.Size != int64(len(want)) {
		t.Fatalf("genuine asset: err %v, %d bytes", err, len(got))
	}

	// Tampered content must be rejected and never reach the destination.
	tampered := releaseServer(t, helmDir, map[string][]byte{helmFile: append(append([]byte{}, want...), 'x')})
	var buf bytes.Buffer
	_, err = c.Download(tampered.URL+"/"+helmFile, &buf, ghrelease.DownloadOptions{})
	if !errors.Is(err, ghrelease.ErrSignatureInvalid) || buf.Len() != 0 {
		t.Fatalf("tampered asset: err %v with %d bytes written", err, buf.Len())
	}

	// And DownloadToFile leaves nothing behind.
	dest := filepath.Join(t.TempDir(), "out")
	if _, err := c.DownloadToFile(tampered.URL+"/"+helmFile, dest, 0o644, ghrelease.DownloadOptions{}); !errors.Is(err, ghrelease.ErrSignatureInvalid) {
		t.Fatalf("DownloadToFile err = %v", err)
	}
	if _, err := os.Stat(dest); err == nil {
		t.Error("tampered asset was written to the destination")
	}
}

func TestPGP_PinnedToWrongKeyRejects(t *testing.T) {
	srv := releaseServer(t, helmDir, nil)
	// A different maintainer in KEYS, who did not sign this release.
	v := helmVerifier(t, pgp.WithFingerprints("967F8AC5E2216F9F4FD270AD92AA783CBAAE8E3B"))
	c := ghrelease.NewClient("", ghrelease.WithManifestVerifier(v))
	if _, err := c.FetchChecksums(context.Background(), srv.URL+"/"+helmFile); !errors.Is(err, ghrelease.ErrSignatureInvalid) {
		t.Fatalf("err = %v, want ErrSignatureInvalid", err)
	}
}

func TestPGP_MissingSignatureFailsClosed(t *testing.T) {
	// A manifest with no ".asc" published next to it.
	srv := releaseServer(t, t.TempDir(), map[string][]byte{"x.sha256sum": []byte("00  x\n")})
	c := ghrelease.NewClient("", ghrelease.WithManifestVerifier(helmVerifier(t)))
	if _, err := c.FetchChecksums(context.Background(), srv.URL+"/x.sha256sum"); !errors.Is(err, ghrelease.ErrSignatureMissing) {
		t.Fatalf("err = %v, want ErrSignatureMissing", err)
	}
}

func TestSigstore_AsManifestVerifier(t *testing.T) {
	good := releaseServer(t, grDir, nil)
	c := ghrelease.NewClient("", ghrelease.WithManifestVerifier(goreleaserVerifier(t, grSubject)))

	sums, err := c.FetchChecksums(context.Background(), good.URL+"/"+grManifest)
	if err != nil {
		t.Fatalf("genuine goreleaser manifest rejected: %v", err)
	}
	if len(sums["goreleaser_Linux_x86_64.tar.gz"]) != 64 {
		t.Errorf("checksum not parsed from %d entries", len(sums))
	}

	tampered := releaseServer(t, grDir, map[string][]byte{
		grManifest: append([]byte("00  evil.tar.gz\n"), read(t, grDir+"/"+grManifest)...),
	})
	sums, err = c.FetchChecksums(context.Background(), tampered.URL+"/"+grManifest)
	if !errors.Is(err, ghrelease.ErrSignatureInvalid) || sums != nil {
		t.Fatalf("tampered manifest: got (%v, %v), want (nil, ErrSignatureInvalid)", sums, err)
	}
}

func TestSigstore_WrongIdentityRejects(t *testing.T) {
	srv := releaseServer(t, grDir, nil)
	v := goreleaserVerifier(t, `^https://github\.com/someone-else/repo/\.github/workflows/release\.yml@refs/tags/v.+$`)
	c := ghrelease.NewClient("", ghrelease.WithManifestVerifier(v))
	if _, err := c.FetchChecksums(context.Background(), srv.URL+"/"+grManifest); !errors.Is(err, ghrelease.ErrSignatureInvalid) {
		t.Fatalf("err = %v, want ErrSignatureInvalid", err)
	}
}

func TestSigstore_AsArtifactVerifier(t *testing.T) {
	srv := releaseServer(t, grDir, nil)
	c := ghrelease.NewClient("", ghrelease.WithArtifactVerifier(goreleaserVerifier(t, grSubject)))
	want := read(t, grDir+"/"+grManifest)

	got, _, err := c.DownloadToBytes(srv.URL+"/"+grManifest, ghrelease.DownloadOptions{})
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("genuine asset: err %v, %d bytes", err, len(got))
	}

	tampered := releaseServer(t, grDir, map[string][]byte{grManifest: want[:len(want)-1]})
	var buf bytes.Buffer
	_, err = c.Download(tampered.URL+"/"+grManifest, &buf, ghrelease.DownloadOptions{})
	if !errors.Is(err, ghrelease.ErrSignatureInvalid) || buf.Len() != 0 {
		t.Fatalf("truncated asset: err %v with %d bytes written", err, buf.Len())
	}
}

// One verifier value serves both roles on a single client.
func TestOneVerifierBothRoles(t *testing.T) {
	srv := releaseServer(t, helmDir, nil)
	v := helmVerifier(t)
	c := ghrelease.NewClient("", ghrelease.WithManifestVerifier(v), ghrelease.WithArtifactVerifier(v))

	if _, err := c.FetchChecksums(context.Background(), srv.URL+"/"+helmFile); err != nil {
		t.Fatalf("manifest role: %v", err)
	}
	if _, _, err := c.DownloadToBytes(srv.URL+"/"+helmFile, ghrelease.DownloadOptions{}); err != nil {
		t.Fatalf("artifact role: %v", err)
	}
}
