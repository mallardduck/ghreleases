// Package ghrelease provides primitives for fetching and extracting GitHub release assets.
package ghrelease

import (
	"errors"
)

// Package errors
var (
	ErrInvalidSource     = errors.New("invalid GitHub source format")
	ErrReleaseNotFound   = errors.New("release not found")
	ErrAssetNotFound     = errors.New("asset not found")
	ErrChecksumMismatch  = errors.New("checksum verification failed")
	ErrUnknownVariable   = errors.New("unknown template variable")
	ErrUnknownModifier   = errors.New("unknown template modifier")
	ErrInvalidModifier   = errors.New("invalid modifier syntax")
	ErrMultipleFiles     = errors.New("archive contains multiple files, ExtractPath required")
	ErrFileNotFound      = errors.New("file not found in archive")
	ErrUnsupportedFormat = errors.New("unsupported archive format")
)
