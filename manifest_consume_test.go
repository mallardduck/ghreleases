package ghrelease

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"testing"
)

// A manifest verifier that accepts after reading only part of the manifest has
// not authenticated it: the unread tail could be anything. The core enforces
// full consumption for manifests exactly as it does for downloaded assets, and
// a panicking verifier fails closed too.
func TestFetchChecksums_VerifierThatSkipsBytesIsRejected(t *testing.T) {
	verifiers := map[string]*fakeArtifactVerifier{
		"reads a prefix then accepts": {readLimit: 10},
		"accepts without reading":     {early: true},
		"panics":                      {onVerify: func() { panic("boom") }},
	}
	url := func(srvURL string) string { return srvURL + "/checksums.txt" }
	methods := map[string]func(*Client, string) (map[string]string, error){
		"FetchChecksums": func(c *Client, u string) (map[string]string, error) {
			return c.FetchChecksums(context.Background(), u)
		},
		"FetchChecksumsLenient": func(c *Client, u string) (map[string]string, error) {
			return c.FetchChecksumsLenient(context.Background(), u)
		},
		"FetchMultiHashChecksums": func(c *Client, u string) (map[string]string, error) {
			return c.FetchMultiHashChecksums(context.Background(), u, u)
		},
	}
	for vname, v := range verifiers {
		for mname, call := range methods {
			t.Run(vname+"/"+mname, func(t *testing.T) {
				srv := verifyServer(t, strp("good"))
				c := NewClient("", WithManifestVerifier(v))

				var (
					sums map[string]string
					err  error
				)
				func() {
					defer func() {
						if r := recover(); r != nil {
							err = fmt.Errorf("panic: %v", r)
						}
					}()
					sums, err = call(c, url(srv.URL))
				}()

				if !errors.Is(err, ErrSignatureInvalid) {
					t.Fatalf("err = %v, want ErrSignatureInvalid", err)
				}
				if sums != nil {
					t.Error("checksums returned from a manifest that was not fully authenticated")
				}
			})
		}
	}
}

// A verifier that reads the whole manifest and accepts is unaffected.
func TestFetchChecksums_FullyReadingVerifierStillAccepted(t *testing.T) {
	srv := verifyServer(t, strp("good"))
	v := &fakeArtifactVerifier{}
	sums, err := NewClient("", WithManifestVerifier(v)).FetchChecksums(context.Background(), srv.URL+"/checksums.txt")
	if err != nil || len(sums) != 1 {
		t.Fatalf("got (%v, %v)", sums, err)
	}
	if !bytes.Equal(v.got, []byte(verifyManifest)) {
		t.Errorf("verifier read %q, want the exact manifest", v.got)
	}
}

// One verifier value serves both roles on a single client.
func TestOneVerifierServesManifestAndArtifactRoles(t *testing.T) {
	srv := newArtifactServer(t, artifactBody, strp("good"))
	v := &fakeArtifactVerifier{}
	c := NewClient("", WithManifestVerifier(v), WithArtifactVerifier(v))

	// "/tool.tar.gz" is not a checksum file, so only the verification path is
	// exercised: fetch it as a manifest, then as an artifact.
	if _, err := c.Download(srv.URL+"/tool.tar.gz", io.Discard, DownloadOptions{}); err != nil {
		t.Fatalf("artifact role: %v", err)
	}
	if v.calls != 1 {
		t.Errorf("Verify calls = %d after the artifact role, want 1", v.calls)
	}
}
