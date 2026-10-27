package ghrelease

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// An opted-in verifier that is nil can never verify anything, so using it must
// fail closed instead of silently skipping verification. The realistic way to
// get one is ignoring NewVerifier's error: that yields a typed-nil pointer
// wrapped in a non-nil interface.
func TestWithManifestVerifier_NilFailsClosed(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		_, _ = w.Write([]byte(verifyManifest))
	}))
	defer srv.Close()
	url := srv.URL + "/checksums.txt"

	var typedNil *fakeVerifier
	for name, v := range map[string]ManifestVerifier{
		"untyped nil": nil,
		"typed nil":   typedNil,
	} {
		for method, call := range map[string]func(*Client) (map[string]string, error){
			"FetchChecksums":        func(c *Client) (map[string]string, error) { return c.FetchChecksums(context.Background(), url) },
			"FetchChecksumsLenient": func(c *Client) (map[string]string, error) { return c.FetchChecksumsLenient(context.Background(), url) },
			"FetchMultiHashChecksums": func(c *Client) (map[string]string, error) {
				return c.FetchMultiHashChecksums(context.Background(), url, url)
			},
		} {
			t.Run(name+"/"+method, func(t *testing.T) {
				before := requests.Load()
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
					sums, err = call(c)
				}()

				if !errors.Is(err, ErrInvalidVerifier) {
					t.Fatalf("err = %v, want ErrInvalidVerifier", err)
				}
				if sums != nil {
					t.Error("checksums returned although the configured verifier is unusable")
				}
				if got := requests.Load() - before; got != 0 {
					t.Errorf("made %d request(s); a misconfigured verifier must fail before any network I/O", got)
				}
			})
		}
	}
}

// A later valid option replaces an earlier invalid one, like any other Option.
func TestWithManifestVerifier_LaterValidOptionWins(t *testing.T) {
	srv := verifyServer(t, strp("good"))
	c := NewClient("", WithManifestVerifier(nil), WithManifestVerifier(&fakeVerifier{}))
	if _, err := c.FetchChecksums(context.Background(), srv.URL+"/checksums.txt"); err != nil {
		t.Fatalf("FetchChecksums() = %v", err)
	}
}
