package ghrelease

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// ParseChecksumFile parses GNU coreutils style checksum files.
// Format: <hash>  <filename>
// Supports both single and double space separators.
// Ignores blank lines and comments (lines starting with #).
func ParseChecksumFile(r io.Reader) (map[string]string, error) {
	checksums := make(map[string]string)
	scanner := bufio.NewScanner(r)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Split on whitespace (handles both single and double space)
		parts := strings.Fields(line)
		if len(parts) < 2 {
			return nil, fmt.Errorf("invalid checksum format at line %d: expected '<hash> <filename>'", lineNum)
		}

		hash := parts[0]
		filename := parts[1]

		// Validate hash is hex (SHA-256 = 64 chars)
		if len(hash) != 64 {
			return nil, fmt.Errorf("invalid hash length at line %d: expected 64 chars, got %d", lineNum, len(hash))
		}

		// Validate hex characters
		for _, c := range hash {
			if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
				return nil, fmt.Errorf("invalid hash at line %d: non-hex character %c", lineNum, c)
			}
		}

		checksums[filename] = strings.ToLower(hash)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading checksum file: %w", err)
	}

	return checksums, nil
}

// ValidateHash compares a computed hash against an expected hash.
// Both hashes are normalized to lowercase for comparison.
func ValidateHash(computed, expected string) error {
	computed = strings.ToLower(strings.TrimSpace(computed))
	expected = strings.ToLower(strings.TrimSpace(expected))

	if computed != expected {
		return fmt.Errorf("%w: expected %s, got %s", ErrChecksumMismatch, expected, computed)
	}

	return nil
}

// isHex64 reports whether s is a 64-character hex digest.
func isHex64(s string) bool {
	if len(s) != 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}
	return true
}

// ParseChecksumFileLenient parses checksum files tolerantly. Unlike
// ParseChecksumFile it never fails on malformed content:
//   - blank lines, "#" comments, and lines whose first field is not a 64-char
//     hex digest are skipped silently;
//   - the hash is lower-cased;
//   - the filename is the last field, with a leading "*" (binary-mode marker)
//     stripped;
//   - a line holding only a hash stores the digest under the key "" (used by
//     single-entry .sha256sum files).
//
// The only error returned is a read error from r.
func ParseChecksumFileLenient(r io.Reader) (map[string]string, error) {
	checksums := make(map[string]string)
	scanner := bufio.NewScanner(r)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if !isHex64(fields[0]) {
			continue
		}
		name := ""
		if len(fields) > 1 {
			name = strings.TrimPrefix(fields[len(fields)-1], "*")
		}
		checksums[name] = strings.ToLower(fields[0])
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading checksum file: %w", err)
	}
	return checksums, nil
}

// ParseHashOrder parses a hash-order file. Stub.
func ParseHashOrder(r io.Reader) ([]string, error) { return nil, nil }

// ParseMultiHashChecksumFile parses multi-hash checksum files. Stub.
func ParseMultiHashChecksumFile(r io.Reader, order []string) (map[string]string, error) {
	return nil, nil
}
