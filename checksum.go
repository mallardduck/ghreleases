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
