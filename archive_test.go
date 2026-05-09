package ghrelease

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"errors"
	"testing"
)

func TestDetectFormat(t *testing.T) {
	tests := []struct {
		filename string
		want     ArchiveFormat
	}{
		{"file.tar.gz", FormatTarGz},
		{"file.tgz", FormatTgz},
		{"file.zip", FormatZip},
		{"file.gz", FormatGzip},
		{"file.txt", FormatPlain},
		{"FILE.TAR.GZ", FormatTarGz}, // uppercase
		{"FILE.ZIP", FormatZip},
		{"archive.tar.gz.sig", FormatPlain}, // doesn't end with .tar.gz
		{"", FormatPlain},
	}

	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			got := detectFormat(tt.filename)
			if got != tt.want {
				t.Errorf("detectFormat(%s) = %v, want %v", tt.filename, got, tt.want)
			}
		})
	}
}

func TestExtractPlain(t *testing.T) {
	data := []byte("plain text content")

	result, err := Extract(data, "file.txt", ExtractOptions{})
	if err != nil {
		t.Errorf("Extract() unexpected error = %v", err)
		return
	}

	if !bytes.Equal(result, data) {
		t.Errorf("Extract() plain file not preserved")
	}
}

func TestExtractGzip(t *testing.T) {
	original := []byte("gzip compressed content")
	compressed := gzipCompress(t, original)

	result, err := Extract(compressed, "file.gz", ExtractOptions{})
	if err != nil {
		t.Errorf("Extract() unexpected error = %v", err)
		return
	}

	if !bytes.Equal(result, original) {
		t.Errorf("Extract() gzip content mismatch")
	}
}

func TestExtractTarGzSingleFile(t *testing.T) {
	content := []byte("binary content")
	archive := createTarGz(t, map[string][]byte{
		"binary": content,
	})

	result, err := Extract(archive, "archive.tar.gz", ExtractOptions{})
	if err != nil {
		t.Errorf("Extract() unexpected error = %v", err)
		return
	}

	if !bytes.Equal(result, content) {
		t.Errorf("Extract() tar.gz content mismatch")
	}
}

func TestExtractTarGzMultipleFilesNoPath(t *testing.T) {
	archive := createTarGz(t, map[string][]byte{
		"file1": []byte("content1"),
		"file2": []byte("content2"),
	})

	_, err := Extract(archive, "archive.tar.gz", ExtractOptions{})
	if err == nil {
		t.Errorf("Extract() expected error for multiple files without ExtractPath")
		return
	}

	if !errors.Is(err, ErrMultipleFiles) {
		t.Errorf("Extract() error = %v, want %v", err, ErrMultipleFiles)
	}
}

func TestExtractTarGzSpecificFile(t *testing.T) {
	files := map[string][]byte{
		"dir/file1": []byte("content1"),
		"dir/file2": []byte("content2"),
		"dir/file3": []byte("content3"),
	}
	archive := createTarGz(t, files)

	tests := []struct {
		name        string
		extractPath string
		wantContent []byte
		wantErr     error
	}{
		{
			name:        "full path",
			extractPath: "dir/file2",
			wantContent: []byte("content2"),
		},
		{
			name:        "base name",
			extractPath: "file2",
			wantContent: []byte("content2"),
		},
		{
			name:        "not found",
			extractPath: "nonexistent",
			wantErr:     ErrFileNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := Extract(archive, "archive.tar.gz", ExtractOptions{
				ExtractPath: tt.extractPath,
			})

			if tt.wantErr != nil {
				if err == nil {
					t.Errorf("Extract() error = nil, wantErr %v", tt.wantErr)
					return
				}
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("Extract() error = %v, wantErr %v", err, tt.wantErr)
				}
				return
			}

			if err != nil {
				t.Errorf("Extract() unexpected error = %v", err)
				return
			}

			if !bytes.Equal(result, tt.wantContent) {
				t.Errorf("Extract() content mismatch")
			}
		})
	}
}

func TestExtractTarGzWithDirectories(t *testing.T) {
	// Create tar.gz with directories (should be skipped)
	var buf bytes.Buffer
	gzw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gzw)

	// Add directory entry
	_ = tw.WriteHeader(&tar.Header{
		Name:     "dir/",
		Typeflag: tar.TypeDir,
		Mode:     0755,
	})

	// Add file entry
	content := []byte("file content")
	_ = tw.WriteHeader(&tar.Header{
		Name: "dir/file",
		Size: int64(len(content)),
		Mode: 0644,
	})
	_, _ = tw.Write(content)

	_ = tw.Close()
	_ = gzw.Close()

	result, err := Extract(buf.Bytes(), "archive.tar.gz", ExtractOptions{})
	if err != nil {
		t.Errorf("Extract() unexpected error = %v", err)
		return
	}

	if !bytes.Equal(result, content) {
		t.Errorf("Extract() content mismatch with directories")
	}
}

func TestExtractZipSingleFile(t *testing.T) {
	content := []byte("zip content")
	archive := createZip(t, map[string][]byte{
		"file": content,
	})

	result, err := Extract(archive, "archive.zip", ExtractOptions{})
	if err != nil {
		t.Errorf("Extract() unexpected error = %v", err)
		return
	}

	if !bytes.Equal(result, content) {
		t.Errorf("Extract() zip content mismatch")
	}
}

func TestExtractZipMultipleFiles(t *testing.T) {
	archive := createZip(t, map[string][]byte{
		"file1": []byte("content1"),
		"file2": []byte("content2"),
	})

	_, err := Extract(archive, "archive.zip", ExtractOptions{})
	if err == nil {
		t.Errorf("Extract() expected error for multiple files without ExtractPath")
		return
	}

	if !errors.Is(err, ErrMultipleFiles) {
		t.Errorf("Extract() error = %v, want %v", err, ErrMultipleFiles)
	}
}

func TestExtractZipSpecificFile(t *testing.T) {
	files := map[string][]byte{
		"dir/file1": []byte("content1"),
		"dir/file2": []byte("content2"),
	}
	archive := createZip(t, files)

	result, err := Extract(archive, "archive.zip", ExtractOptions{
		ExtractPath: "dir/file2",
	})
	if err != nil {
		t.Errorf("Extract() unexpected error = %v", err)
		return
	}

	if !bytes.Equal(result, []byte("content2")) {
		t.Errorf("Extract() content mismatch")
	}
}

func TestExtractFormatOverride(t *testing.T) {
	content := []byte("content")
	archive := createTarGz(t, map[string][]byte{
		"file": content,
	})

	// File has wrong extension, but we specify format explicitly
	result, err := Extract(archive, "wrong.zip", ExtractOptions{
		Format: FormatTarGz,
	})
	if err != nil {
		t.Errorf("Extract() unexpected error = %v", err)
		return
	}

	if !bytes.Equal(result, content) {
		t.Errorf("Extract() format override failed")
	}
}

func TestExtractInvalidArchive(t *testing.T) {
	tests := []struct {
		name     string
		data     []byte
		filename string
	}{
		{
			name:     "invalid gzip",
			data:     []byte("not a gzip file"),
			filename: "file.gz",
		},
		{
			name:     "invalid tar.gz",
			data:     []byte("not a tar.gz file"),
			filename: "file.tar.gz",
		},
		{
			name:     "invalid zip",
			data:     []byte("not a zip file"),
			filename: "file.zip",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Extract(tt.data, tt.filename, ExtractOptions{})
			if err == nil {
				t.Errorf("Extract() expected error for invalid archive")
			}
		})
	}
}

func TestExtractEmptyArchive(t *testing.T) {
	// Create empty tar.gz
	emptyTarGz := createTarGz(t, map[string][]byte{})

	_, err := Extract(emptyTarGz, "empty.tar.gz", ExtractOptions{})
	if err == nil {
		t.Errorf("Extract() expected error for empty archive")
	}
}

func TestExtractUnsupportedFormat(t *testing.T) {
	data := []byte("content")

	_, err := Extract(data, "file.rar", ExtractOptions{
		Format: ArchiveFormat("rar"),
	})
	if err == nil {
		t.Errorf("Extract() expected error for unsupported format")
		return
	}

	if !errors.Is(err, ErrUnsupportedFormat) {
		t.Errorf("Extract() error = %v, want %v", err, ErrUnsupportedFormat)
	}
}

// Helper functions

func gzipCompress(t *testing.T, data []byte) []byte {
	var buf bytes.Buffer
	gzw := gzip.NewWriter(&buf)
	if _, err := gzw.Write(data); err != nil {
		t.Fatalf("gzip write failed: %v", err)
	}
	if err := gzw.Close(); err != nil {
		t.Fatalf("gzip close failed: %v", err)
	}
	return buf.Bytes()
}

func createTarGz(t *testing.T, files map[string][]byte) []byte {
	var buf bytes.Buffer
	gzw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gzw)

	for name, content := range files {
		hdr := &tar.Header{
			Name: name,
			Size: int64(len(content)),
			Mode: 0644,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("tar write header failed: %v", err)
		}
		if _, err := tw.Write(content); err != nil {
			t.Fatalf("tar write failed: %v", err)
		}
	}

	if err := tw.Close(); err != nil {
		t.Fatalf("tar close failed: %v", err)
	}
	if err := gzw.Close(); err != nil {
		t.Fatalf("gzip close failed: %v", err)
	}

	return buf.Bytes()
}

func createZip(t *testing.T, files map[string][]byte) []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("zip create failed: %v", err)
		}
		if _, err := w.Write(content); err != nil {
			t.Fatalf("zip write failed: %v", err)
		}
	}

	if err := zw.Close(); err != nil {
		t.Fatalf("zip close failed: %v", err)
	}

	return buf.Bytes()
}
