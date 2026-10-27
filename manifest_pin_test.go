package ghrelease

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// These pin today's behavior: without a verifier, the FetchChecksums* methods
// make exactly one request, for the manifest, and never look for a signature.
func TestFetchChecksums_WithoutVerifier_OnlyRequestsManifest(t *testing.T) {
	const manifest = "5f42bb2ed72df51df4277fabf6bea980035dbd5b144c5b276e80c165b449910b  tool_1.0.0_linux_amd64.tar.gz\n"

	var mu sync.Mutex
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		if r.URL.Path != "/checksums.txt" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(manifest))
	}))
	defer srv.Close()

	c := NewClient("")
	for name, fetch := range map[string]func() (map[string]string, error){
		"strict": func() (map[string]string, error) {
			return c.FetchChecksums(context.Background(), srv.URL+"/checksums.txt")
		},
		"lenient": func() (map[string]string, error) {
			return c.FetchChecksumsLenient(context.Background(), srv.URL+"/checksums.txt")
		},
	} {
		mu.Lock()
		paths = nil
		mu.Unlock()

		sums, err := fetch()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(sums) != 1 {
			t.Errorf("%s: got %d checksums, want 1", name, len(sums))
		}
		mu.Lock()
		got := append([]string(nil), paths...)
		mu.Unlock()
		if len(got) != 1 || got[0] != "/checksums.txt" {
			t.Errorf("%s: requests = %v, want only /checksums.txt", name, got)
		}
	}
}
