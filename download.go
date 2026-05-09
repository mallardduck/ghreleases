package ghrelease

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
)

// Download fetches an asset and writes it to w while computing SHA-256.
// Always computes hash (returned in result).
// If opts.ExpectedHash is set, validates before returning.
func (c *Client) Download(url string, w io.Writer, opts DownloadOptions) (*DownloadResult, error) {
	ctx := opts.Context
	if ctx == nil {
		ctx = context.Background()
	}

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	req.Header.Set("Accept", "application/octet-stream")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download failed: status %d", resp.StatusCode)
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

// DownloadToBytes is a convenience wrapper that downloads to a byte slice.
func (c *Client) DownloadToBytes(url string, opts DownloadOptions) ([]byte, *DownloadResult, error) {
	var buf bytes.Buffer
	result, err := c.Download(url, &buf, opts)
	if err != nil {
		return nil, nil, err
	}
	return buf.Bytes(), result, nil
}
