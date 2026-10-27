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
	"path/filepath"
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
	// signature is expected next to the asset (see SignatureVerifier.SignatureName).
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

// errVerifierFinished is what the verifier's stream reports once Verify has
// returned. A write that fails with it means the verifier stopped reading before
// the end of the artifact.
var errVerifierFinished = errors.New("verifier returned before reading the whole artifact")

// Download fetches an asset and writes it to w while computing SHA-256.
// Always computes hash (returned in result).
//
// Verification is opt-in. If opts.ExpectedHash is set, or a verifier was
// configured with WithArtifactVerifier, every check must pass or an error is
// returned; the signature is fetched before the asset, so a missing one fails
// without downloading it. While a check is pending, w receives nothing: the
// asset is held in a temporary file (see opts.TempDir) and copied to w only once
// all checks pass. Set opts.StreamUnverified to write as bytes arrive instead.
// With nothing to verify, bytes stream straight to w.
func (c *Client) Download(url string, w io.Writer, opts DownloadOptions) (*DownloadResult, error) {
	return c.download(url, w, opts, !opts.StreamUnverified)
}

// download implements Download. hold reports whether the asset must be held
// back from w until the checks pass; it only has an effect when there are checks.
func (c *Client) download(url string, w io.Writer, opts DownloadOptions, hold bool) (*DownloadResult, error) {
	if c.artifactVerifierErr != nil {
		return nil, c.artifactVerifierErr
	}
	ctx := opts.Context
	if ctx == nil {
		ctx = context.Background()
	}

	// Fetch the signature first: if it is missing there is nothing to verify the
	// asset against, and the (possibly large) asset is never downloaded.
	var sig []byte
	if c.artifactVerifier != nil {
		sigURL := opts.SignatureURL
		if sigURL == "" {
			sigURL = siblingURL(url, c.artifactVerifier.SignatureName(lastPathSegment(url)))
		}
		var err error
		if sig, err = c.fetchSignature(ctx, sigURL); err != nil {
			return nil, err
		}
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

	hasChecks := c.artifactVerifier != nil || opts.ExpectedHash != ""
	dst := w
	var spool *os.File
	if hold && hasChecks {
		if spool, err = os.CreateTemp(opts.TempDir, "ghreleases-*"); err != nil {
			return nil, fmt.Errorf("download failed: create temporary file: %w", err)
		}
		defer func() {
			_ = spool.Close()
			_ = os.Remove(spool.Name())
		}()
		dst = spool
	}

	hasher := sha256.New()
	sinks := []io.Writer{dst, hasher}
	var vr *verifyRun
	if c.artifactVerifier != nil {
		vr = startVerify(ctx, c.artifactVerifier, sig)
		sinks = append(sinks, vr.pw)
	}

	size, copyErr := io.Copy(io.MultiWriter(sinks...), resp.Body)

	if vr != nil {
		// Always release the verifier: end of stream on success, the cause on failure.
		if copyErr != nil {
			_ = vr.pw.CloseWithError(copyErr)
		} else {
			_ = vr.pw.Close()
		}
		verdict, err := vr.wait(ctx)
		if err != nil {
			return nil, fmt.Errorf("download failed: %w", err)
		}
		if copyErr != nil && errors.Is(copyErr, errVerifierFinished) {
			// The verifier stopped reading: it either rejected, or accepted
			// without authenticating the whole artifact. Neither is a pass.
			if verdict == nil {
				verdict = errVerifierFinished
			}
			return nil, &wrapped{sentinel: ErrSignatureInvalid, cause: verdict}
		}
		if copyErr == nil && verdict != nil {
			return nil, &wrapped{sentinel: ErrSignatureInvalid, cause: verdict}
		}
	}
	if copyErr != nil {
		return nil, fmt.Errorf("download failed: %w", copyErr)
	}

	computedHash := hex.EncodeToString(hasher.Sum(nil))

	// Validate if expected hash provided
	if opts.ExpectedHash != "" {
		if err := ValidateHash(computedHash, opts.ExpectedHash); err != nil {
			return nil, err
		}
	}

	// Every check passed: only now does the destination get the asset.
	if spool != nil {
		if _, err := spool.Seek(0, io.SeekStart); err != nil {
			return nil, fmt.Errorf("download failed: %w", err)
		}
		if _, err := io.Copy(w, spool); err != nil {
			return nil, fmt.Errorf("download failed: %w", err)
		}
	}

	return &DownloadResult{
		Hash: computedHash,
		Size: size,
	}, nil
}

// verifyRun is a verifier running against a pipe that the download writes to.
// io.Pipe writes complete only once the reader has consumed the bytes, so a
// verifier that stops reading early makes the download's write fail rather than
// silently authenticating a prefix.
type verifyRun struct {
	pw   *io.PipeWriter
	done chan error
}

func startVerify(ctx context.Context, v SignatureVerifier, sig []byte) *verifyRun {
	pr, pw := io.Pipe()
	r := &verifyRun{pw: pw, done: make(chan error, 1)}
	go func() {
		err := safeVerify(ctx, v, pr, sig)
		// Whatever the verdict, the verifier is done with the stream: unblock the
		// writer so one that returned early cannot stall the download.
		_ = pr.CloseWithError(errVerifierFinished)
		r.done <- err
	}()
	return r
}

// wait returns the verifier's verdict, or ctx's error if it never finishes.
func (r *verifyRun) wait(ctx context.Context) (verdict error, err error) {
	select {
	case verdict = <-r.done:
		return verdict, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// DownloadToFile downloads an asset to path, creating the file with perm only
// after every requested check (ExpectedHash, signature) has passed. On any
// failure path is left exactly as it was: an existing file is not replaced and
// no partial or temporary file remains. The asset is held in a temporary file in
// path's directory and renamed into place, so it is never visible half-written.
// opts.StreamUnverified and opts.TempDir do not apply.
func (c *Client) DownloadToFile(url, path string, perm os.FileMode, opts DownloadOptions) (*DownloadResult, error) {
	if c.artifactVerifierErr != nil {
		return nil, c.artifactVerifierErr
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".ghreleases-*")
	if err != nil {
		return nil, fmt.Errorf("download failed: create temporary file: %w", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }() // no-op once renamed into place

	// The temporary file is the hold, so write to it directly.
	res, err := c.download(url, tmp, opts, false)
	if cerr := tmp.Close(); err == nil && cerr != nil {
		err = fmt.Errorf("download failed: %w", cerr)
	}
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(tmp.Name(), perm); err != nil {
		return nil, fmt.Errorf("download failed: %w", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return nil, fmt.Errorf("download failed: %w", err)
	}
	return res, nil
}

// DownloadToBytes is a convenience wrapper that downloads to a byte slice.
func (c *Client) DownloadToBytes(url string, opts DownloadOptions) ([]byte, *DownloadResult, error) {
	var buf bytes.Buffer
	// The buffer is discarded on any failure, so there is nothing to hold back.
	opts.StreamUnverified = true
	result, err := c.Download(url, &buf, opts)
	if err != nil {
		return nil, nil, err
	}
	return buf.Bytes(), result, nil
}
