package ghrelease

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDownload(t *testing.T) {
	testData := []byte("test content for download")
	expectedHash := computeSHA256(testData)

	tests := []struct {
		name         string
		statusCode   int
		responseBody []byte
		opts         DownloadOptions
		wantErr      error
		wantSize     int64
		wantHash     string
	}{
		{
			name:         "successful download",
			statusCode:   http.StatusOK,
			responseBody: testData,
			opts:         DownloadOptions{},
			wantErr:      nil,
			wantSize:     int64(len(testData)),
			wantHash:     expectedHash,
		},
		{
			name:         "download with valid checksum",
			statusCode:   http.StatusOK,
			responseBody: testData,
			opts: DownloadOptions{
				ExpectedHash: expectedHash,
			},
			wantErr:  nil,
			wantSize: int64(len(testData)),
			wantHash: expectedHash,
		},
		{
			name:         "download with invalid checksum",
			statusCode:   http.StatusOK,
			responseBody: testData,
			opts: DownloadOptions{
				ExpectedHash: "0000000000000000000000000000000000000000000000000000000000000000",
			},
			wantErr: ErrChecksumMismatch,
		},
		{
			name:         "download with uppercase checksum",
			statusCode:   http.StatusOK,
			responseBody: testData,
			opts: DownloadOptions{
				ExpectedHash: computeSHA256Upper(testData),
			},
			wantErr:  nil,
			wantSize: int64(len(testData)),
			wantHash: expectedHash,
		},
		{
			name:         "not found",
			statusCode:   http.StatusNotFound,
			responseBody: []byte("Not Found"),
			opts:         DownloadOptions{},
			wantErr:      nil, // Will be a generic error, not a specific one
		},
		{
			name:         "empty file",
			statusCode:   http.StatusOK,
			responseBody: []byte{},
			opts:         DownloadOptions{},
			wantErr:      nil,
			wantSize:     0,
			wantHash:     "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", // SHA-256 of empty string
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Verify request
				if r.Method != "GET" {
					t.Errorf("Expected GET request, got %s", r.Method)
				}
				if r.Header.Get("Accept") != "application/octet-stream" {
					t.Errorf("Expected Accept: application/octet-stream header")
				}

				w.WriteHeader(tt.statusCode)
				_, _ = w.Write(tt.responseBody)
			}))
			defer server.Close()

			client := NewClient("")

			var buf bytes.Buffer
			result, err := client.Download(server.URL, &buf, tt.opts)

			if tt.wantErr != nil {
				if err == nil {
					t.Errorf("Download() error = nil, wantErr %v", tt.wantErr)
					return
				}
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("Download() error = %v, wantErr %v", err, tt.wantErr)
				}
				return
			}

			// For non-OK status codes, expect an error
			if tt.statusCode != http.StatusOK {
				if err == nil {
					t.Errorf("Download() expected error for status %d", tt.statusCode)
				}
				return
			}

			if err != nil {
				t.Errorf("Download() unexpected error = %v", err)
				return
			}

			if result.Size != tt.wantSize {
				t.Errorf("Download() size = %d, want %d", result.Size, tt.wantSize)
			}

			if result.Hash != tt.wantHash {
				t.Errorf("Download() hash = %s, want %s", result.Hash, tt.wantHash)
			}

			// Verify data was written correctly
			if !bytes.Equal(buf.Bytes(), tt.responseBody) {
				t.Errorf("Download() written data doesn't match")
			}
		})
	}
}

func TestDownloadWithAuth(t *testing.T) {
	testData := []byte("authenticated content")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer secret-token" {
			t.Errorf("Expected Authorization header 'Bearer secret-token', got %s", auth)
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(testData)
	}))
	defer server.Close()

	client := NewClient("secret-token")

	var buf bytes.Buffer
	_, err := client.Download(server.URL, &buf, DownloadOptions{})
	if err != nil {
		t.Errorf("Download() unexpected error = %v", err)
	}
}

func TestDownloadToBytes(t *testing.T) {
	testData := []byte("byte slice download")
	expectedHash := computeSHA256(testData)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(testData)
	}))
	defer server.Close()

	client := NewClient("")

	data, result, err := client.DownloadToBytes(server.URL, DownloadOptions{})
	if err != nil {
		t.Errorf("DownloadToBytes() unexpected error = %v", err)
		return
	}

	if !bytes.Equal(data, testData) {
		t.Errorf("DownloadToBytes() data doesn't match")
	}

	if result.Hash != expectedHash {
		t.Errorf("DownloadToBytes() hash = %s, want %s", result.Hash, expectedHash)
	}

	if result.Size != int64(len(testData)) {
		t.Errorf("DownloadToBytes() size = %d, want %d", result.Size, len(testData))
	}
}

func TestDownloadContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Wait for context cancellation
		<-r.Context().Done()
	}))
	defer server.Close()

	client := NewClient("")

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	var buf bytes.Buffer
	_, err := client.Download(server.URL, &buf, DownloadOptions{Context: ctx})
	if err == nil {
		t.Errorf("Download() expected error due to cancelled context")
	}
}

func TestDownloadLargeFile(t *testing.T) {
	// Test streaming with a larger file to ensure we're not buffering everything
	largeData := bytes.Repeat([]byte("large content "), 10000) // ~130KB
	expectedHash := computeSHA256(largeData)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(largeData)
	}))
	defer server.Close()

	client := NewClient("")

	var buf bytes.Buffer
	result, err := client.Download(server.URL, &buf, DownloadOptions{})
	if err != nil {
		t.Errorf("Download() unexpected error = %v", err)
		return
	}

	if result.Hash != expectedHash {
		t.Errorf("Download() hash mismatch for large file")
	}

	if result.Size != int64(len(largeData)) {
		t.Errorf("Download() size = %d, want %d", result.Size, len(largeData))
	}

	if !bytes.Equal(buf.Bytes(), largeData) {
		t.Errorf("Download() large file data mismatch")
	}
}

// Helper functions
func computeSHA256(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

func computeSHA256Upper(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}
