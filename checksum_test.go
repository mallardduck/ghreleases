package ghrelease

import (
	"errors"
	"strings"
	"testing"
)

func TestParseChecksumFile(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		want     map[string]string
		wantErr  bool
		errCheck func(error) bool
	}{
		{
			name: "valid single file",
			input: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855  file.txt",
			want: map[string]string{
				"file.txt": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
			},
		},
		{
			name: "valid multiple files",
			input: `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855  file1.txt
abc0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855  file2.txt
def0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855  file3.txt`,
			want: map[string]string{
				"file1.txt": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
				"file2.txt": "abc0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
				"file3.txt": "def0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
			},
		},
		{
			name: "single space separator",
			input: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855 file.txt",
			want: map[string]string{
				"file.txt": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
			},
		},
		{
			name: "uppercase hash normalized to lowercase",
			input: "E3B0C44298FC1C149AFBF4C8996FB92427AE41E4649B934CA495991B7852B855  file.txt",
			want: map[string]string{
				"file.txt": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
			},
		},
		{
			name: "mixed case hash",
			input: "E3b0C44298Fc1C149afBf4c8996FB92427ae41E4649b934ca495991b7852B855  file.txt",
			want: map[string]string{
				"file.txt": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
			},
		},
		{
			name: "comments and blank lines",
			input: `# This is a comment
e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855  file1.txt

# Another comment
abc0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855  file2.txt

`,
			want: map[string]string{
				"file1.txt": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
				"file2.txt": "abc0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
			},
		},
		{
			name:    "empty file",
			input:   "",
			want:    map[string]string{},
			wantErr: false,
		},
		{
			name: "only comments",
			input: `# Comment 1
# Comment 2`,
			want: map[string]string{},
		},
		{
			name:    "invalid format - no space",
			input:   "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
			wantErr: true,
		},
		{
			name:    "invalid format - only hash",
			input:   "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855  ",
			wantErr: true,
		},
		{
			name:    "invalid hash length - too short",
			input:   "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b  file.txt",
			wantErr: true,
		},
		{
			name:    "invalid hash length - too long",
			input:   "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855aa  file.txt",
			wantErr: true,
		},
		{
			name:    "invalid hash - non-hex characters",
			input:   "g3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855  file.txt",
			wantErr: true,
		},
		{
			name:    "invalid hash - special characters",
			input:   "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b85!  file.txt",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := strings.NewReader(tt.input)
			got, err := ParseChecksumFile(r)

			if tt.wantErr {
				if err == nil {
					t.Errorf("ParseChecksumFile() error = nil, wantErr true")
				}
				return
			}

			if err != nil {
				t.Errorf("ParseChecksumFile() unexpected error = %v", err)
				return
			}

			if len(got) != len(tt.want) {
				t.Errorf("ParseChecksumFile() got %d entries, want %d", len(got), len(tt.want))
				return
			}

			for filename, wantHash := range tt.want {
				gotHash, ok := got[filename]
				if !ok {
					t.Errorf("ParseChecksumFile() missing filename %s", filename)
					continue
				}
				if gotHash != wantHash {
					t.Errorf("ParseChecksumFile() hash for %s = %s, want %s", filename, gotHash, wantHash)
				}
			}
		})
	}
}

func TestValidateHash(t *testing.T) {
	tests := []struct {
		name     string
		computed string
		expected string
		wantErr  error
	}{
		{
			name:     "matching hashes",
			computed: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
			expected: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
			wantErr:  nil,
		},
		{
			name:     "matching hashes with whitespace",
			computed: "  e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855  ",
			expected: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
			wantErr:  nil,
		},
		{
			name:     "case insensitive - lowercase vs uppercase",
			computed: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
			expected: "E3B0C44298FC1C149AFBF4C8996FB92427AE41E4649B934CA495991B7852B855",
			wantErr:  nil,
		},
		{
			name:     "case insensitive - mixed case",
			computed: "E3b0C44298Fc1C149afBf4c8996FB92427ae41E4649b934ca495991b7852B855",
			expected: "e3B0c44298fC1c149AFbF4C8996fb92427AE41e4649B934CA495991B7852b855",
			wantErr:  nil,
		},
		{
			name:     "mismatched hashes",
			computed: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
			expected: "abc0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
			wantErr:  ErrChecksumMismatch,
		},
		{
			name:     "mismatched hashes - single char difference",
			computed: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
			expected: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b856",
			wantErr:  ErrChecksumMismatch,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateHash(tt.computed, tt.expected)

			if tt.wantErr != nil {
				if err == nil {
					t.Errorf("ValidateHash() error = nil, wantErr %v", tt.wantErr)
					return
				}
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("ValidateHash() error = %v, wantErr %v", err, tt.wantErr)
				}
				return
			}

			if err != nil {
				t.Errorf("ValidateHash() unexpected error = %v", err)
			}
		})
	}
}
