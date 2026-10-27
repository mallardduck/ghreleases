package ghrelease

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
)

// DownloadOptions configures asset download behavior.
type DownloadOptions struct {
	// If set, validates downloaded content against this hash (SHA-256 hex, case-insensitive)
	ExpectedHash string

	// Context for cancellation (optional)
	Context context.Context

	// SignatureURL is the exact URL of the asset's detached signature when a
	// verifier is configured via WithArtifactVerifier. Needed for GitHub API
	// asset URLs, whose last path segment is an opaque ID. When empty, the
	// signature is expected next to the asset (see ArtifactVerifier.SignatureName).
	SignatureURL string

	// StreamUnverified is an escape hatch. By default, when ExpectedHash is set
	// or a verifier is configured, the destination receives no bytes until every
	// check has passed. Setting this writes bytes to the destination as they
	// arrive; the checks still run, but on failure the destination already holds
	// unverified data and the caller must discard it.
	StreamUnverified bool

	// TempDir is where bytes are held until verified. Defaults to os.TempDir().
	TempDir string
}

// DownloadResult contains the outcome of a download operation.
type DownloadResult struct {
	Hash string // Computed SHA-256 hex string
	Size int64  // Bytes downloaded
}

// Download fetches an asset and writes it to w while computing SHA-256.
// Always computes hash (returned in result).
// If opts.ExpectedHash is set, validates before returning.
func (c *Client) Download(url string, w io.Writer, opts DownloadOptions) (*DownloadResult, error) {
	ctx := opts.Context
	if ctx == nil {
		ctx = context.Background()
	}

	// No timeout is applied here: the body is bounded only by ctx so large
	// artifacts are not cut off mid-stream.
	req, err := c.newRequest(ctx, url, "application/octet-stream")
	if err != nil {
		return nil, err
	}

	resp, err := c.send(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, &HTTPStatusError{URL: url, StatusCode: resp.StatusCode}
	}

	// Stream to writer while computing hash
	hasher := sha256.New()
	tee := io.TeeReader(resp.Body, hasher)

	size, err := io.Copy(w, tee)
	if err != nil {
		return nil, fmt.Errorf("download failed: %w", err)
	}

	computedHash := hex.EncodeToString(hasher.Sum(nil))

	// Validate if expected hash provided
	if opts.ExpectedHash != "" {
		if err := ValidateHash(computedHash, opts.ExpectedHash); err != nil {
			return nil, err
		}
	}

	return &DownloadResult{
		Hash: computedHash,
		Size: size,
	}, nil
}

// DownloadToFile downloads an asset to path, creating the file with perm only
// after every requested check (ExpectedHash, signature) has passed. On any
// failure path is left exactly as it was: an existing file is not replaced and
// no partial or temporary file remains. Stub: not implemented yet.
func (c *Client) DownloadToFile(url, path string, perm os.FileMode, opts DownloadOptions) (*DownloadResult, error) {
	return nil, errors.New("DownloadToFile: not implemented")
}

// DownloadToBytes is a convenience wrapper that downloads to a byte slice.
func (c *Client) DownloadToBytes(url string, opts DownloadOptions) ([]byte, *DownloadResult, error) {
	var buf bytes.Buffer
	result, err := c.Download(url, &buf, opts)
	if err != nil {
		return nil, nil, err
	}
	return buf.Bytes(), result, nil
}
