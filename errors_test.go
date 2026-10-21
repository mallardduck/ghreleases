package ghrelease

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestErrorsIsAndAs(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	c := NewClient("", WithBaseURL(srv.URL))

	check := func(name string, err error, sentinel error) {
		t.Helper()
		var se *HTTPStatusError
		if !errors.Is(err, sentinel) {
			t.Errorf("%s: errors.Is(%v) = false", name, sentinel)
		}
		if !errors.As(err, &se) || se.StatusCode != http.StatusNotFound || se.URL == "" {
			t.Errorf("%s: errors.As HTTPStatusError failed: %v", name, err)
		}
		if !strings.HasPrefix(err.Error(), sentinel.Error()+": unexpected status code: 404") {
			t.Errorf("%s: message = %q", name, err.Error())
		}
	}

	_, err := c.GetRelease(context.Background(), "o", "r", "v1")
	check("GetRelease", err, ErrReleaseNotFound)
	_, err = c.ResolveTagCommit(context.Background(), "o", "r", "v1")
	check("ResolveTagCommit", err, ErrRefNotFound)

	_, err = c.Download(srv.URL, io.Discard, DownloadOptions{})
	var se *HTTPStatusError
	if !errors.As(err, &se) || se.StatusCode != 404 {
		t.Errorf("Download error = %v", err)
	}
	_, err = c.FetchChecksums(context.Background(), srv.URL)
	if !errors.As(err, &se) {
		t.Errorf("FetchChecksums error = %v", err)
	}
}

func TestWrappedDoesNotMatchOtherSentinels(t *testing.T) {
	err := &wrapped{sentinel: ErrRefNotFound, cause: errors.New("boom")}
	if errors.Is(err, ErrReleaseNotFound) {
		t.Error("wrapped matched unrelated sentinel")
	}
}
