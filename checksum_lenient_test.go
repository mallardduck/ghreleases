package ghrelease

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseChecksumFileLenient(t *testing.T) {
	h1 := strings.Repeat("a", 64)
	h2 := strings.Repeat("B", 64)
	h3 := strings.Repeat("c", 64)
	input := strings.Join([]string{
		"# comment",
		"",
		"not a hash line",
		"deadbeef  short.bin",
		h1 + "  plain.tar.gz",
		h2 + " *binary.zip",
		h3,
		"   ",
	}, "\n")

	got, err := ParseChecksumFileLenient(strings.NewReader(input))
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	want := map[string]string{
		"plain.tar.gz": h1,
		"binary.zip":   strings.ToLower(h2),
		"":             h3,
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("got[%q] = %q, want %q", k, got[k], v)
		}
	}
}

func TestParseChecksumFileLenient_LastFieldIsName(t *testing.T) {
	h := strings.Repeat("1", 64)
	got, _ := ParseChecksumFileLenient(strings.NewReader(h + " extra mid file.bin\n"))
	if got["file.bin"] != h {
		t.Errorf("got %v", got)
	}
}

func TestParseChecksumFile_StillStrict(t *testing.T) {
	if _, err := ParseChecksumFile(strings.NewReader("garbage line here\n")); err == nil {
		t.Error("strict parser accepted garbage")
	}
}


func TestFetchChecksumsLenient(t *testing.T) {
	h := strings.Repeat("e", 64)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "junk\n"+h+"\n")
	}))
	defer srv.Close()

	got, err := NewClient("").FetchChecksumsLenient(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if got[""] != h {
		t.Errorf("got %v", got)
	}
	// Strict fetch of the same body must still fail.
	if _, err := NewClient("").FetchChecksums(context.Background(), srv.URL); err == nil {
		t.Error("FetchChecksums accepted junk")
	}
}
