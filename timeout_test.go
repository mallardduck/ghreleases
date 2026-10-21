package ghrelease

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDefaultClientHasNoTotalTimeout(t *testing.T) {
	if NewClient("").httpClient.Timeout != 0 {
		t.Error("default client must not set a total timeout")
	}
}

func TestDownloadSlowBody(t *testing.T) {
	// A body that streams over time must not be cut off by the client itself.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fl := w.(http.Flusher)
		for i := 0; i < 5; i++ {
			_, _ = io.WriteString(w, strings.Repeat("x", 1024))
			fl.Flush()
			time.Sleep(50 * time.Millisecond)
		}
	}))
	defer srv.Close()

	var buf bytes.Buffer
	res, err := NewClient("").Download(srv.URL, &buf, DownloadOptions{})
	if err != nil || res.Size != 5*1024 {
		t.Fatalf("res=%v err=%v", res, err)
	}
}

func TestWithDefaultTimeout(t *testing.T) {
	ctx, cancel := withDefaultTimeout(context.Background())
	defer cancel()
	if _, ok := ctx.Deadline(); !ok {
		t.Error("no deadline applied to context without one")
	}

	parent, pcancel := context.WithTimeout(context.Background(), time.Hour)
	defer pcancel()
	got, c2 := withDefaultTimeout(parent)
	defer c2()
	d1, _ := parent.Deadline()
	if d2, _ := got.Deadline(); !d1.Equal(d2) {
		t.Error("caller deadline was replaced")
	}
}

func TestOptionsVariadicCompat(t *testing.T) {
	_ = NewClient("")
	_ = NewClientWithHTTP(&http.Client{}, "")
	c := NewClient("", WithBaseURL("http://x/"), WithAllowedHosts(" GitHub.com ", ""))
	if c.baseURL != "http://x" || len(c.allowedHosts) != 1 || c.allowedHosts[0] != "github.com" {
		t.Errorf("unexpected options state: %+v", c)
	}
}
