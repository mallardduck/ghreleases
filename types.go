package ghrelease

import (
	"context"
	"errors"
	"net/http"
)

// Client handles GitHub API interactions.
type Client struct {
	httpClient *http.Client
	token      string // optional GITHUB_TOKEN
	baseURL    string // defaults to "https://api.github.com"
}

// Release represents a GitHub release.
type Release struct {
	TagName string
	Assets  []Asset
}

// Asset represents a downloadable release asset.
type Asset struct {
	Name string
	URL  string // API URL for downloading
	Size int64
}

// DownloadOptions configures asset download behavior.
type DownloadOptions struct {
	// If set, validates downloaded content against this hash (SHA-256 hex, case-insensitive)
	ExpectedHash string

	// Context for cancellation (optional)
	Context context.Context
}

// TemplateVars holds variables for template rendering.
type TemplateVars struct {
	Name    string // Binary name
	Version string // Release version/tag
	OS      string // Operating system (linux, darwin, windows)
	Arch    string // Architecture (amd64, arm64, etc)
	Ext     string // File extension (tar.gz, zip, etc)
}

// TemplateMode controls error handling for unknown variables.
type TemplateMode int

const (
	// TemplateStrict returns error on unknown variables/modifiers
	TemplateStrict TemplateMode = iota

	// TemplatePermissive passes through unknown vars/modifiers unchanged
	TemplatePermissive
)

// ArchiveFormat represents supported archive types.
type ArchiveFormat string

const (
	FormatTarGz ArchiveFormat = "tar.gz"
	FormatTgz   ArchiveFormat = "tgz"
	FormatZip   ArchiveFormat = "zip"
	FormatGzip  ArchiveFormat = "gz"
	FormatPlain ArchiveFormat = "plain" // no archive
)

// ExtractOptions configures archive extraction behavior.
type ExtractOptions struct {
	// Format override (auto-detect from filename if empty)
	Format ArchiveFormat

	// Specific file path within archive (empty = auto-select single file)
	ExtractPath string
}

// DownloadResult contains the outcome of a download operation.
type DownloadResult struct {
	Hash string // Computed SHA-256 hex string
	Size int64  // Bytes downloaded
}

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
