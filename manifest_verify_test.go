package ghrelease

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

const verifyManifest = "5f42bb2ed72df51df4277fabf6bea980035dbd5b144c5b276e80c165b449910b  tool_1.0.0_linux_amd64.tar.gz\n"

// fakeVerifier accepts a manifest only when the signature bytes equal "good".
type fakeVerifier struct {
	sawManifest []byte
	calls       int
}

func (f *fakeVerifier) SignatureName(manifestName string) string { return manifestName + ".sig" }

func (f *fakeVerifier) Verify(_ context.Context, content io.Reader, signature []byte) error {
	manifest, err := io.ReadAll(content)
	if err != nil {
		return err
	}
	f.calls++
	f.sawManifest = manifest
	if string(signature) != "good" {
		return errors.New("bad signature")
	}
	return nil
}

func verifyServer(t *testing.T, sig *string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/checksums.txt", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(verifyManifest))
	})
	mux.HandleFunc("/checksums.txt.sig", func(w http.ResponseWriter, r *http.Request) {
		if sig == nil {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(*sig))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func strp(s string) *string { return &s }

func TestFetchChecksums_ManifestVerifier(t *testing.T) {
	tests := []struct {
		name    string
		sig     *string
		wantErr error
	}{
		{"valid signature", strp("good"), nil},
		{"invalid signature", strp("evil"), ErrSignatureInvalid},
		{"missing signature fails closed", nil, ErrSignatureMissing},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := verifyServer(t, tt.sig)
			v := &fakeVerifier{}
			c := NewClient("", WithManifestVerifier(v))

			sums, err := c.FetchChecksums(context.Background(), srv.URL+"/checksums.txt")
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("err = %v, want %v", err, tt.wantErr)
				}
				if sums != nil {
					t.Error("checksums must not be returned when verification fails")
				}
				return
			}
			if err != nil {
				t.Fatalf("FetchChecksums() error = %v", err)
			}
			if len(sums) != 1 {
				t.Errorf("got %d checksums, want 1", len(sums))
			}
			if string(v.sawManifest) != verifyManifest {
				t.Errorf("verifier saw %q, want the exact manifest bytes", v.sawManifest)
			}
		})
	}
}

func TestFetchChecksums_NoVerifierDoesNotFetchSignature(t *testing.T) {
	srv := verifyServer(t, nil) // signature would 404
	c := NewClient("")
	if _, err := c.FetchChecksums(context.Background(), srv.URL+"/checksums.txt"); err != nil {
		t.Fatalf("default behavior must be unchanged, got %v", err)
	}
}

func TestFetchChecksumsLenient_ManifestVerifier(t *testing.T) {
	srv := verifyServer(t, strp("evil"))
	c := NewClient("", WithManifestVerifier(&fakeVerifier{}))
	if _, err := c.FetchChecksumsLenient(context.Background(), srv.URL+"/checksums.txt"); !errors.Is(err, ErrSignatureInvalid) {
		t.Fatalf("err = %v, want ErrSignatureInvalid", err)
	}
}

func TestFetchMultiHashChecksums_ManifestVerifier(t *testing.T) {
	srv := verifyServer(t, strp("evil"))
	c := NewClient("", WithManifestVerifier(&fakeVerifier{}))
	_, err := c.FetchMultiHashChecksums(context.Background(), srv.URL+"/checksums.txt", srv.URL+"/checksums.txt")
	if !errors.Is(err, ErrSignatureInvalid) {
		t.Fatalf("err = %v, want ErrSignatureInvalid", err)
	}
}

func TestFetchChecksums_SignatureURLOverride(t *testing.T) {
	// GitHub API asset URLs end in an opaque ID, so the signature cannot be
	// found by name next to the manifest; callers pass its exact URL.
	mux := http.NewServeMux()
	mux.HandleFunc("/assets/101", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(verifyManifest)) })
	mux.HandleFunc("/assets/102", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("good")) })
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := NewClient("", WithManifestVerifier(&fakeVerifier{}))

	// Without the override the derived URL (/assets/101.sig) does not exist.
	_, err := c.FetchChecksums(context.Background(), srv.URL+"/assets/101")
	if !errors.Is(err, ErrSignatureMissing) {
		t.Fatalf("derived lookup err = %v, want ErrSignatureMissing", err)
	}

	ctx := WithSignatureURL(context.Background(), srv.URL+"/assets/102")
	if _, err := c.FetchChecksums(ctx, srv.URL+"/assets/101"); err != nil {
		t.Fatalf("with override: %v", err)
	}
}
