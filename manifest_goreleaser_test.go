package ghrelease

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// goreleaserVerifier mimics the Sigstore verifier's naming convention, and
// records what it was asked to authenticate.
type goreleaserVerifier struct {
	manifest []byte
	bundle   []byte
	err      error
}

func (v *goreleaserVerifier) SignatureName(m string) string { return m + ".sigstore.json" }

func (v *goreleaserVerifier) Verify(_ context.Context, content io.Reader, sig []byte) error {
	manifest, err := io.ReadAll(content)
	if err != nil {
		return err
	}
	v.manifest, v.bundle = manifest, sig
	return v.err
}

// goreleaserServer reproduces how GitHub serves goreleaser v2.19.0: the release
// lists assets whose API URLs end in an opaque numeric ID, and the bytes are
// only returned for "Accept: application/octet-stream".
func goreleaserServer(t *testing.T, manifest, bundle []byte) *httptest.Server {
	t.Helper()
	const (
		manifestID = "667103704"
		bundleID   = "667103721"
	)
	var srv *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/goreleaser/goreleaser/releases/tags/v2.19.0", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintf(w, `{"tag_name":"v2.19.0","assets":[
			{"name":"checksums.txt","url":"%[1]s/repos/goreleaser/goreleaser/releases/assets/%[2]s","size":%[4]d},
			{"name":"checksums.txt.sigstore.json","url":"%[1]s/repos/goreleaser/goreleaser/releases/assets/%[3]s","size":%[5]d}]}`,
			srv.URL, manifestID, bundleID, len(manifest), len(bundle))
	})
	asset := func(body []byte) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Accept") != "application/octet-stream" {
				http.Error(w, "asset needs Accept: application/octet-stream", http.StatusUnsupportedMediaType)
				return
			}
			_, _ = w.Write(body)
		}
	}
	mux.HandleFunc("/repos/goreleaser/goreleaser/releases/assets/"+manifestID, asset(manifest))
	if bundle != nil {
		mux.HandleFunc("/repos/goreleaser/goreleaser/releases/assets/"+bundleID, asset(bundle))
	}
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func assetURL(t *testing.T, rel *Release, name string) string {
	t.Helper()
	for _, a := range rel.Assets {
		if a.Name == name {
			return a.URL
		}
	}
	t.Fatalf("release has no asset %q", name)
	return ""
}

func readGoreleaserManifest(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/goreleaser-v2.19.0/checksums.txt")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestFetchChecksums_GoreleaserReleaseLayout(t *testing.T) {
	manifest := readGoreleaserManifest(t)
	srv := goreleaserServer(t, manifest, []byte(`{"bundle":"stand-in"}`))
	v := &goreleaserVerifier{}
	c := NewClient("", WithManifestVerifier(v))
	c.baseURL = srv.URL
	ctx := context.Background()

	rel, err := c.GetRelease(ctx, "goreleaser", "goreleaser", "v2.19.0")
	if err != nil {
		t.Fatal(err)
	}
	manifestURL := assetURL(t, rel, "checksums.txt")

	// Opaque asset URLs: the sibling-by-name guess cannot work.
	if _, err := c.FetchChecksums(ctx, manifestURL); !errors.Is(err, ErrSignatureMissing) {
		t.Fatalf("without WithSignatureURL err = %v, want ErrSignatureMissing", err)
	}

	sigURL := assetURL(t, rel, v.SignatureName("checksums.txt"))
	sums, err := c.FetchChecksums(WithSignatureURL(ctx, sigURL), manifestURL)
	if err != nil {
		t.Fatalf("FetchChecksums: %v", err)
	}
	if string(v.manifest) != string(manifest) {
		t.Error("verifier did not receive the exact manifest bytes")
	}
	if string(v.bundle) != `{"bundle":"stand-in"}` {
		t.Errorf("verifier got bundle %q", v.bundle)
	}
	if got := sums["goreleaser_Linux_x86_64.tar.gz"]; len(got) != 64 {
		t.Errorf("real goreleaser manifest not parsed: Linux_x86_64 = %q (%d checksums)", got, len(sums))
	}
}

func TestFetchChecksums_GoreleaserRejectedBundleYieldsNothing(t *testing.T) {
	srv := goreleaserServer(t, readGoreleaserManifest(t), []byte("bundle"))
	v := &goreleaserVerifier{err: errors.New("certificate identity mismatch")}
	c := NewClient("", WithManifestVerifier(v))
	c.baseURL = srv.URL
	ctx := context.Background()

	rel, err := c.GetRelease(ctx, "goreleaser", "goreleaser", "v2.19.0")
	if err != nil {
		t.Fatal(err)
	}
	ctx = WithSignatureURL(ctx, assetURL(t, rel, "checksums.txt.sigstore.json"))
	sums, err := c.FetchChecksums(ctx, assetURL(t, rel, "checksums.txt"))
	if !errors.Is(err, ErrSignatureInvalid) || sums != nil {
		t.Fatalf("got (%v, %v), want (nil, ErrSignatureInvalid)", sums, err)
	}
	if !strings.Contains(err.Error(), "certificate identity mismatch") {
		t.Errorf("verifier's reason lost from error: %v", err)
	}
}

// When the release has no bundle at all (e.g. an older release from before
// signing was enabled), opting in to verification must fail, not downgrade.
func TestFetchChecksums_GoreleaserUnsignedReleaseFailsClosed(t *testing.T) {
	srv := goreleaserServer(t, readGoreleaserManifest(t), nil)
	c := NewClient("", WithManifestVerifier(&goreleaserVerifier{}))
	c.baseURL = srv.URL
	ctx := WithSignatureURL(context.Background(), srv.URL+"/repos/goreleaser/goreleaser/releases/assets/667103721")

	sums, err := c.FetchChecksums(ctx, srv.URL+"/repos/goreleaser/goreleaser/releases/assets/667103704")
	if !errors.Is(err, ErrSignatureMissing) || sums != nil {
		t.Fatalf("got (%v, %v), want (nil, ErrSignatureMissing)", sums, err)
	}
}

// yq signs only its multi-hash "checksums" file. Its "checksums_hashes_order"
// has no signature of its own, so a verifier must not demand one for it.
func TestFetchMultiHashChecksums_VerifiesManifestOnly(t *testing.T) {
	manifest, err := os.ReadFile("testdata/yq-v4.54.1/checksums")
	if err != nil {
		t.Fatal(err)
	}
	order, err := os.ReadFile("testdata/yq-v4.54.1/checksums_hashes_order")
	if err != nil {
		t.Fatal(err)
	}
	var sigRequests []string
	mux := http.NewServeMux()
	mux.HandleFunc("/checksums", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(manifest) })
	mux.HandleFunc("/checksums_hashes_order", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(order) })
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		sigRequests = append(sigRequests, r.URL.Path)
		if r.URL.Path == "/checksums.bundle" {
			_, _ = w.Write([]byte("bundle"))
			return
		}
		http.NotFound(w, r)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	v := &yqVerifier{}
	c := NewClient("", WithManifestVerifier(v))
	sums, err := c.FetchMultiHashChecksums(context.Background(), srv.URL+"/checksums", srv.URL+"/checksums_hashes_order")
	if err != nil {
		t.Fatalf("FetchMultiHashChecksums: %v", err)
	}
	if len(sums) == 0 {
		t.Fatal("no checksums parsed")
	}
	if len(sigRequests) != 1 || sigRequests[0] != "/checksums.bundle" {
		t.Errorf("signature requests = %v, want only /checksums.bundle", sigRequests)
	}
}

type yqVerifier struct{}

func (yqVerifier) SignatureName(m string) string { return m + ".bundle" }
func (yqVerifier) Verify(_ context.Context, content io.Reader, _ []byte) error {
	_, err := io.Copy(io.Discard, content)
	return err
}
