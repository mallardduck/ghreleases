package ghrelease

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// artifactBody is larger than any internal pipe or copy buffer, so a verifier
// that is handed only the first chunk is distinguishable from one that sees
// the whole stream.
var artifactBody = bytes.Repeat([]byte("0123456789abcdef"), 3<<16) // 3 MiB

// fakeArtifactVerifier decides by signature content ("good" accepts) after
// reading the artifact, unless told to misbehave.
type fakeArtifactVerifier struct {
	mu sync.Mutex

	readLimit int64        // if > 0, read only this many bytes before deciding
	early     bool         // decide without reading anything
	earlyErr  error        // verdict when early (nil means "accept")
	onVerify  func()       // called before any read
	got       []byte       // everything the verifier read
	sig       []byte       // signature it was given
	calls     int          // times Verify was called
	ctxErr    func() error // set by Verify: reports ctx.Err() at the end
}

func (f *fakeArtifactVerifier) SignatureName(name string) string { return name + ".sig" }

func (f *fakeArtifactVerifier) Verify(ctx context.Context, r io.Reader, sig []byte) error {
	f.mu.Lock()
	f.calls++
	f.sig = append([]byte(nil), sig...)
	hook := f.onVerify
	f.mu.Unlock()
	if hook != nil {
		hook()
	}
	if f.early {
		return f.earlyErr
	}
	src := r
	if f.readLimit > 0 {
		src = io.LimitReader(r, f.readLimit)
	}
	b, err := io.ReadAll(src)
	f.mu.Lock()
	f.got = b
	f.mu.Unlock()
	if err != nil {
		return err
	}
	if string(sig) != "good" {
		return errors.New("bad signature")
	}
	return nil
}

// artifactServer serves one asset two ways: by friendly name, and by opaque
// GitHub-API-style ID which, like GitHub, needs Accept: application/octet-stream.
type artifactServer struct {
	*httptest.Server
	bodyHits int32
	sigHits  int32
}

func newArtifactServer(t *testing.T, body []byte, sig *string) *artifactServer {
	t.Helper()
	s := &artifactServer{}
	asset := func(b []byte, hits *int32) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(hits, 1)
			if b == nil {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write(b)
		}
	}
	needsAccept := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Accept") != "application/octet-stream" {
				http.Error(w, "need Accept: application/octet-stream", http.StatusUnsupportedMediaType)
				return
			}
			h(w, r)
		}
	}
	var sigBytes []byte
	if sig != nil {
		sigBytes = []byte(*sig)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/tool.tar.gz", asset(body, &s.bodyHits))
	mux.HandleFunc("/tool.tar.gz.sig", asset(sigBytes, &s.sigHits))
	mux.HandleFunc("/assets/201", needsAccept(asset(body, &s.bodyHits)))
	mux.HandleFunc("/assets/202", needsAccept(asset(sigBytes, &s.sigHits)))
	s.Server = httptest.NewServer(mux)
	t.Cleanup(s.Close)
	return s
}

func (s *artifactServer) bodies() int { return int(atomic.LoadInt32(&s.bodyHits)) }
func (s *artifactServer) sigs() int   { return int(atomic.LoadInt32(&s.sigHits)) }

func TestDownload_ArtifactVerifier(t *testing.T) {
	wantHash := computeSHA256(artifactBody)
	tests := []struct {
		name       string
		sig        *string
		wantErr    error
		wantBodies int
	}{
		{"valid signature", strp("good"), nil, 1},
		{"invalid signature", strp("evil"), ErrSignatureInvalid, 1},
		{"missing signature fails before any body is downloaded", nil, ErrSignatureMissing, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newArtifactServer(t, artifactBody, tt.sig)
			c := NewClient("", WithArtifactVerifier(&fakeArtifactVerifier{}))

			var buf bytes.Buffer
			res, err := c.Download(srv.URL+"/tool.tar.gz", &buf, DownloadOptions{})

			if got := srv.bodies(); got != tt.wantBodies {
				t.Errorf("body requests = %d, want %d", got, tt.wantBodies)
			}
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("err = %v, want %v", err, tt.wantErr)
				}
				if res != nil {
					t.Error("result returned although verification failed")
				}
				if buf.Len() != 0 {
					t.Errorf("destination received %d unverified bytes", buf.Len())
				}
				return
			}
			if err != nil {
				t.Fatalf("Download() = %v", err)
			}
			if !bytes.Equal(buf.Bytes(), artifactBody) {
				t.Error("verified artifact was not written intact")
			}
			if res.Hash != wantHash || res.Size != int64(len(artifactBody)) {
				t.Errorf("result = %+v, want hash %s size %d", res, wantHash, len(artifactBody))
			}
		})
	}
}

func TestDownload_ArtifactVerifierSeesEveryByteOnce(t *testing.T) {
	srv := newArtifactServer(t, artifactBody, strp("good"))
	v := &fakeArtifactVerifier{}
	c := NewClient("", WithArtifactVerifier(v))

	if _, err := c.Download(srv.URL+"/tool.tar.gz", io.Discard, DownloadOptions{}); err != nil {
		t.Fatal(err)
	}
	if v.calls != 1 {
		t.Errorf("Verify called %d times, want 1", v.calls)
	}
	if !bytes.Equal(v.got, artifactBody) {
		t.Errorf("verifier read %d bytes, want the exact %d-byte artifact", len(v.got), len(artifactBody))
	}
	if string(v.sig) != "good" {
		t.Errorf("verifier got signature %q", v.sig)
	}
}

// A verifier that accepts after reading only a prefix has not authenticated the
// artifact. The core, not each verifier, must refuse that.
func TestDownload_VerifierThatSkipsBytesIsRejected(t *testing.T) {
	for name, v := range map[string]*fakeArtifactVerifier{
		"reads a prefix then accepts": {readLimit: 1000},
		"accepts without reading":     {early: true},
	} {
		t.Run(name, func(t *testing.T) {
			srv := newArtifactServer(t, artifactBody, strp("good"))
			c := NewClient("", WithArtifactVerifier(v))

			var buf bytes.Buffer
			res, err := c.Download(srv.URL+"/tool.tar.gz", &buf, DownloadOptions{})
			if !errors.Is(err, ErrSignatureInvalid) || res != nil {
				t.Fatalf("got (%v, %v), want (nil, ErrSignatureInvalid)", res, err)
			}
			if buf.Len() != 0 {
				t.Errorf("destination received %d bytes from an unauthenticated download", buf.Len())
			}
		})
	}
}

// A panicking verifier must fail the download, not crash the process or let
// the asset through.
func TestDownload_PanickingVerifierFailsClosed(t *testing.T) {
	srv := newArtifactServer(t, artifactBody, strp("good"))
	v := &fakeArtifactVerifier{onVerify: func() { panic("boom") }}
	c := NewClient("", WithArtifactVerifier(v))

	var buf bytes.Buffer
	res, err := c.Download(srv.URL+"/tool.tar.gz", &buf, DownloadOptions{})
	if !errors.Is(err, ErrSignatureInvalid) || res != nil {
		t.Fatalf("got (%v, %v), want (nil, ErrSignatureInvalid)", res, err)
	}
	if buf.Len() != 0 {
		t.Errorf("destination received %d bytes", buf.Len())
	}
}

// A verifier that rejects immediately never drains its input; the download
// must abort rather than block forever on the unread stream.
func TestDownload_EarlyRejectDoesNotHang(t *testing.T) {
	srv := newArtifactServer(t, artifactBody, strp("good"))
	c := NewClient("", WithArtifactVerifier(&fakeArtifactVerifier{early: true, earlyErr: errors.New("nope")}))

	done := make(chan error, 1)
	var buf bytes.Buffer
	go func() {
		_, err := c.Download(srv.URL+"/tool.tar.gz", &buf, DownloadOptions{})
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, ErrSignatureInvalid) {
			t.Fatalf("err = %v, want ErrSignatureInvalid", err)
		}
		if buf.Len() != 0 {
			t.Errorf("destination received %d bytes", buf.Len())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Download hung after the verifier rejected without reading")
	}
}

func TestDownload_ArtifactSignatureURLOverride(t *testing.T) {
	// GitHub API asset URLs end in an opaque ID, so the signature cannot be
	// found by name next to the asset.
	srv := newArtifactServer(t, artifactBody, strp("good"))
	c := NewClient("", WithArtifactVerifier(&fakeArtifactVerifier{}))
	assetURL := srv.URL + "/assets/201"

	_, err := c.Download(assetURL, io.Discard, DownloadOptions{})
	if !errors.Is(err, ErrSignatureMissing) {
		t.Fatalf("derived lookup err = %v, want ErrSignatureMissing", err)
	}

	if _, err := c.Download(assetURL, io.Discard, DownloadOptions{SignatureURL: srv.URL + "/assets/202"}); err != nil {
		t.Fatalf("with SignatureURL: %v", err)
	}
}

func TestDownload_NoArtifactVerifierFetchesNoSignature(t *testing.T) {
	srv := newArtifactServer(t, artifactBody, nil) // a signature would 404
	if _, err := NewClient("").Download(srv.URL+"/tool.tar.gz", io.Discard, DownloadOptions{}); err != nil {
		t.Fatalf("default behavior must be unchanged, got %v", err)
	}
	if srv.sigs() != 0 {
		t.Errorf("fetched the signature %d time(s) with no verifier configured", srv.sigs())
	}
}

func TestDownload_ContextCancelMidStreamWritesNothing(t *testing.T) {
	started := make(chan struct{})
	mux := http.NewServeMux()
	mux.HandleFunc("/tool.tar.gz.sig", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("good")) })
	mux.HandleFunc("/tool.tar.gz", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(artifactBody[:64<<10])
		w.(http.Flusher).Flush()
		close(started)
		select {
		case <-r.Context().Done():
		case <-time.After(10 * time.Second):
		}
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-started
		// Give the client time to read the first chunk, so a destination that
		// is written as bytes arrive would already hold unverified data.
		time.Sleep(300 * time.Millisecond)
		cancel()
	}()

	c := NewClient("", WithArtifactVerifier(&fakeArtifactVerifier{}))
	var buf bytes.Buffer
	res, err := c.Download(srv.URL+"/tool.tar.gz", &buf, DownloadOptions{Context: ctx})
	if err == nil || res != nil {
		t.Fatalf("got (%v, %v), want a cancellation error", res, err)
	}
	if buf.Len() != 0 {
		t.Errorf("destination received %d bytes from a cancelled, unverified download", buf.Len())
	}
}

func TestDownload_ArtifactVerifierWithExpectedHash(t *testing.T) {
	goodHash, badHash := computeSHA256(artifactBody), computeSHA256([]byte("something else"))
	tests := []struct {
		name    string
		sig     string
		hash    string
		wantErr error
	}{
		{"both good", "good", goodHash, nil},
		{"good signature, wrong hash", "good", badHash, ErrChecksumMismatch},
		{"bad signature, right hash", "evil", goodHash, ErrSignatureInvalid},
		{"both bad: signature failure is reported first", "evil", badHash, ErrSignatureInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newArtifactServer(t, artifactBody, strp(tt.sig))
			c := NewClient("", WithArtifactVerifier(&fakeArtifactVerifier{}))
			var buf bytes.Buffer
			_, err := c.Download(srv.URL+"/tool.tar.gz", &buf, DownloadOptions{ExpectedHash: tt.hash})
			if !errors.Is(err, tt.wantErr) && !(tt.wantErr == nil && err == nil) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil && buf.Len() != 0 {
				t.Errorf("destination received %d bytes although a check failed", buf.Len())
			}
			if tt.wantErr == nil && !bytes.Equal(buf.Bytes(), artifactBody) {
				t.Error("verified artifact not written")
			}
		})
	}
}

// The "write nothing until verified" rule covers a plain ExpectedHash too, not
// only signatures.
func TestDownload_ExpectedHashWritesNothingOnMismatch(t *testing.T) {
	srv := newArtifactServer(t, artifactBody, nil)
	var buf bytes.Buffer
	_, err := NewClient("").Download(srv.URL+"/tool.tar.gz", &buf, DownloadOptions{ExpectedHash: computeSHA256([]byte("other"))})
	if !errors.Is(err, ErrChecksumMismatch) {
		t.Fatalf("err = %v, want ErrChecksumMismatch", err)
	}
	if buf.Len() != 0 {
		t.Errorf("destination received %d unverified bytes", buf.Len())
	}

	buf.Reset()
	if _, err := NewClient("").Download(srv.URL+"/tool.tar.gz", &buf, DownloadOptions{ExpectedHash: computeSHA256(artifactBody)}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(buf.Bytes(), artifactBody) {
		t.Error("verified download not written intact")
	}
}

func TestDownload_StreamUnverifiedEscapeHatch(t *testing.T) {
	srv := newArtifactServer(t, artifactBody, strp("evil"))

	t.Run("hash mismatch", func(t *testing.T) {
		var buf bytes.Buffer
		_, err := NewClient("").Download(srv.URL+"/tool.tar.gz", &buf,
			DownloadOptions{ExpectedHash: computeSHA256([]byte("other")), StreamUnverified: true})
		if !errors.Is(err, ErrChecksumMismatch) {
			t.Fatalf("checks must still run: err = %v", err)
		}
		if !bytes.Equal(buf.Bytes(), artifactBody) {
			t.Error("StreamUnverified should have written bytes as they arrived")
		}
	})
	t.Run("bad signature", func(t *testing.T) {
		var buf bytes.Buffer
		c := NewClient("", WithArtifactVerifier(&fakeArtifactVerifier{}))
		_, err := c.Download(srv.URL+"/tool.tar.gz", &buf, DownloadOptions{StreamUnverified: true})
		if !errors.Is(err, ErrSignatureInvalid) {
			t.Fatalf("checks must still run: err = %v", err)
		}
		if !bytes.Equal(buf.Bytes(), artifactBody) {
			t.Error("StreamUnverified should have written bytes as they arrived")
		}
	})
}

// With nothing to verify there is nothing to wait for: bytes must reach the
// destination while the server is still sending, not after a full spool.
func TestDownload_StreamsImmediatelyWhenNothingToVerify(t *testing.T) {
	release := make(chan struct{})
	mux := http.NewServeMux()
	mux.HandleFunc("/tool.tar.gz", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("first-chunk"))
		w.(http.Flusher).Flush()
		select {
		case <-release:
		case <-r.Context().Done():
		}
		_, _ = w.Write([]byte("-last-chunk"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	firstSeen := make(chan struct{})
	var once sync.Once
	w := writerFunc(func(p []byte) (int, error) {
		once.Do(func() { close(firstSeen) })
		return len(p), nil
	})
	done := make(chan error, 1)
	go func() {
		_, err := NewClient("").Download(srv.URL+"/tool.tar.gz", w, DownloadOptions{})
		done <- err
	}()

	select {
	case <-firstSeen:
	case <-time.After(3 * time.Second):
		t.Error("no bytes reached the destination while the server was still sending")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

func TestDownload_SpoolsToConfiguredTempDirAndCleansUp(t *testing.T) {
	for _, sig := range []string{"good", "evil"} {
		t.Run(sig, func(t *testing.T) {
			dir := t.TempDir()
			srv := newArtifactServer(t, artifactBody, strp(sig))

			var duringVerify int
			v := &fakeArtifactVerifier{onVerify: func() {
				entries, _ := os.ReadDir(dir)
				duringVerify = len(entries)
			}}
			c := NewClient("", WithArtifactVerifier(v))
			_, _ = c.Download(srv.URL+"/tool.tar.gz", io.Discard, DownloadOptions{TempDir: dir})

			if duringVerify != 1 {
				t.Errorf("%d entries in TempDir during verification, want the 1 spool file", duringVerify)
			}
			if entries, _ := os.ReadDir(dir); len(entries) != 0 {
				t.Errorf("%d file(s) left behind in TempDir", len(entries))
			}
		})
	}
}

func TestDownloadToFile(t *testing.T) {
	good := computeSHA256(artifactBody)

	t.Run("success writes the file with the requested mode and leaves nothing else", func(t *testing.T) {
		dir := t.TempDir()
		dest := filepath.Join(dir, "tool")
		srv := newArtifactServer(t, artifactBody, strp("good"))
		c := NewClient("", WithArtifactVerifier(&fakeArtifactVerifier{}))

		res, err := c.DownloadToFile(srv.URL+"/tool.tar.gz", dest, 0o755, DownloadOptions{ExpectedHash: good})
		if err != nil {
			t.Fatalf("DownloadToFile() = %v", err)
		}
		if res.Hash != good {
			t.Errorf("hash = %s", res.Hash)
		}
		got, err := os.ReadFile(dest)
		if err != nil || !bytes.Equal(got, artifactBody) {
			t.Fatalf("destination content wrong (err %v)", err)
		}
		if fi, _ := os.Stat(dest); fi.Mode().Perm() != 0o755 {
			t.Errorf("mode = %v, want 0755", fi.Mode().Perm())
		}
		if entries, _ := os.ReadDir(dir); len(entries) != 1 {
			t.Errorf("%d entries in destination dir, want only the file", len(entries))
		}
	})

	failures := []struct {
		name string
		sig  *string
		opts DownloadOptions
		want error
	}{
		{"bad signature", strp("evil"), DownloadOptions{}, ErrSignatureInvalid},
		{"missing signature", nil, DownloadOptions{}, ErrSignatureMissing},
		{"hash mismatch", strp("good"), DownloadOptions{ExpectedHash: computeSHA256([]byte("other"))}, ErrChecksumMismatch},
	}
	for _, tt := range failures {
		t.Run(tt.name+" leaves an existing file untouched", func(t *testing.T) {
			dir := t.TempDir()
			dest := filepath.Join(dir, "tool")
			if err := os.WriteFile(dest, []byte("old"), 0o644); err != nil {
				t.Fatal(err)
			}
			srv := newArtifactServer(t, artifactBody, tt.sig)
			c := NewClient("", WithArtifactVerifier(&fakeArtifactVerifier{}))

			if _, err := c.DownloadToFile(srv.URL+"/tool.tar.gz", dest, 0o755, tt.opts); !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
			if got, _ := os.ReadFile(dest); string(got) != "old" {
				t.Errorf("existing file was modified: %q", got)
			}
			if entries, _ := os.ReadDir(dir); len(entries) != 1 {
				t.Errorf("%d entries left in destination dir, want 1", len(entries))
			}
		})
		t.Run(tt.name+" creates nothing when there was no file", func(t *testing.T) {
			dir := t.TempDir()
			srv := newArtifactServer(t, artifactBody, tt.sig)
			c := NewClient("", WithArtifactVerifier(&fakeArtifactVerifier{}))

			if _, err := c.DownloadToFile(srv.URL+"/tool.tar.gz", filepath.Join(dir, "tool"), 0o755, tt.opts); !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
			if entries, _ := os.ReadDir(dir); len(entries) != 0 {
				t.Errorf("%d file(s) left behind", len(entries))
			}
		})
	}
}

// Opting into artifact verification with a verifier that is nil can never
// verify anything, so every download path must fail closed before any I/O.
func TestWithArtifactVerifier_NilFailsClosed(t *testing.T) {
	srv := newArtifactServer(t, artifactBody, strp("good"))
	var typedNil *fakeArtifactVerifier
	for name, v := range map[string]ArtifactVerifier{"untyped nil": nil, "typed nil": typedNil} {
		t.Run(name, func(t *testing.T) {
			c := NewClient("", WithArtifactVerifier(v))
			url := srv.URL + "/tool.tar.gz"
			dest := filepath.Join(t.TempDir(), "tool")

			calls := map[string]func() error{
				"Download":        func() error { _, err := c.Download(url, io.Discard, DownloadOptions{}); return err },
				"DownloadToBytes": func() error { _, _, err := c.DownloadToBytes(url, DownloadOptions{}); return err },
				"DownloadToFile":  func() error { _, err := c.DownloadToFile(url, dest, 0o644, DownloadOptions{}); return err },
			}
			for method, call := range calls {
				before := srv.bodies() + srv.sigs()
				var err error
				func() {
					defer func() {
						if r := recover(); r != nil {
							err = fmt.Errorf("panic: %v", r)
						}
					}()
					err = call()
				}()
				if !errors.Is(err, ErrInvalidVerifier) {
					t.Errorf("%s: err = %v, want ErrInvalidVerifier", method, err)
				}
				if got := srv.bodies() + srv.sigs() - before; got != 0 {
					t.Errorf("%s: made %d request(s) before failing", method, got)
				}
			}
			if _, err := os.Stat(dest); err == nil {
				t.Error("DownloadToFile created the destination")
			}
		})
	}
}

// The manifest and artifact signature locations are independent: a context
// carrying WithSignatureURL for the manifest must never be used to verify an
// artifact, and vice versa. Both verifiers can also run on one client.
func TestManifestAndArtifactVerifiersAreIndependent(t *testing.T) {
	mux := http.NewServeMux()
	serve := func(path, body string) {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) })
	}
	serve("/assets/301", verifyManifest) // manifest
	serve("/assets/302", "good")         // manifest signature
	serve("/assets/303", "artifact")     // artifact
	serve("/assets/304", "good")         // artifact signature
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := NewClient("", WithManifestVerifier(&fakeVerifier{}), WithArtifactVerifier(&fakeArtifactVerifier{}))
	manifestCtx := WithSignatureURL(context.Background(), srv.URL+"/assets/302")

	if _, err := c.FetchChecksums(manifestCtx, srv.URL+"/assets/301"); err != nil {
		t.Fatalf("manifest: %v", err)
	}
	// Same context, artifact download with no SignatureURL of its own: the
	// manifest's signature URL must not be borrowed (it would say "good").
	_, err := c.Download(srv.URL+"/assets/303", io.Discard, DownloadOptions{Context: manifestCtx})
	if !errors.Is(err, ErrSignatureMissing) {
		t.Fatalf("artifact borrowed the manifest's signature URL: err = %v, want ErrSignatureMissing", err)
	}
	if _, err := c.Download(srv.URL+"/assets/303", io.Discard,
		DownloadOptions{Context: manifestCtx, SignatureURL: srv.URL + "/assets/304"}); err != nil {
		t.Fatalf("artifact with its own SignatureURL: %v", err)
	}
}
