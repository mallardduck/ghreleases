package ghrelease

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func readFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/yq-v4.54.1/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

const (
	yqAmd64SHA256 = "8e34fc298390875de416e6a4afcb8cabeceb25d9aa8506c1a2f9353cf702ea5f"
	yqArm64SHA256 = "189088da0c6429ec5178dfaab1a114805f6cab0b61b165ab236efedf1d57a71b"
)

func TestParseMultiHashChecksumFile_YQ(t *testing.T) {
	order, err := ParseHashOrder(strings.NewReader(readFixture(t, "checksums_hashes_order")))
	if err != nil {
		t.Fatal(err)
	}
	if len(order) != 31 {
		t.Fatalf("order has %d columns, want 31", len(order))
	}

	got, err := ParseMultiHashChecksumFile(strings.NewReader(readFixture(t, "checksums")), order)
	if err != nil {
		t.Fatal(err)
	}
	if got["yq_linux_amd64"] != yqAmd64SHA256 {
		t.Errorf("amd64 = %q, want %q", got["yq_linux_amd64"], yqAmd64SHA256)
	}
	if got["yq_linux_arm64"] != yqArm64SHA256 {
		t.Errorf("arm64 = %q, want %q", got["yq_linux_arm64"], yqArm64SHA256)
	}
	for name, h := range got {
		if strings.HasPrefix(h, "665578f9") {
			t.Errorf("%s selected the SHA3-256 column", name)
		}
	}
}

func TestParseHashOrder_SkipsBlanks(t *testing.T) {
	got, err := ParseHashOrder(strings.NewReader("\nMD5\n  \nSHA-256\r\n\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "MD5" || got[1] != "SHA-256" {
		t.Errorf("got %q", got)
	}
}

func TestParseMultiHashChecksumFile_Rejections(t *testing.T) {
	h := strings.Repeat("a", 64)
	tests := []struct {
		name  string
		order []string
		input string
	}{
		{"no SHA-256 column", []string{"MD5", "SHA3-256"}, "f 1 " + h + "\n"},
		{"shape-only inference is refused (sha-256 case differs)", []string{"sha-256"}, "f " + h + "\n"},
		{"duplicate SHA-256", []string{"SHA-256", "SHA-256"}, "f " + h + " " + h + "\n"},
		{"too few columns", []string{"MD5", "SHA-256"}, "f " + h + "\n"},
		{"too many columns", []string{"SHA-256"}, "f " + h + " " + h + "\n"},
		{"non-hex value", []string{"SHA-256"}, "f " + strings.Repeat("z", 64) + "\n"},
		{"wrong length", []string{"SHA-256"}, "f abcd\n"},
		{"empty order", nil, "f " + h + "\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseMultiHashChecksumFile(strings.NewReader(tt.input), tt.order)
			if !errors.Is(err, ErrChecksumFormat) {
				t.Errorf("error = %v, want ErrChecksumFormat", err)
			}
		})
	}
}

func TestFetchMultiHashChecksums(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/checksums":
			_, _ = io.WriteString(w, readFixture(t, "checksums"))
		case "/order":
			_, _ = io.WriteString(w, readFixture(t, "checksums_hashes_order"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := NewClient("", WithBaseURL(srv.URL))

	got, err := c.FetchMultiHashChecksums(context.Background(), srv.URL+"/checksums", srv.URL+"/order")
	if err != nil {
		t.Fatal(err)
	}
	if got["yq_linux_amd64"] != yqAmd64SHA256 {
		t.Errorf("amd64 = %q", got["yq_linux_amd64"])
	}

	_, err = c.FetchMultiHashChecksums(context.Background(), srv.URL+"/checksums", srv.URL+"/missing")
	var se *HTTPStatusError
	if !errors.As(err, &se) || se.StatusCode != http.StatusNotFound {
		t.Errorf("missing order file: error = %v, want 404 HTTPStatusError", err)
	}
}
