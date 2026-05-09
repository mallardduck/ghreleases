package ghrelease

import (
	"errors"
	"testing"
)

func TestParseSource(t *testing.T) {
	tests := []struct {
		name       string
		source     string
		wantOwner  string
		wantRepo   string
		wantErr    error
		errContains string
	}{
		// Valid formats
		{
			name:      "simple owner/repo",
			source:    "owner/repo",
			wantOwner: "owner",
			wantRepo:  "repo",
		},
		{
			name:      "simple with whitespace",
			source:    "  owner/repo  ",
			wantOwner: "owner",
			wantRepo:  "repo",
		},
		{
			name:      "simple with .git suffix",
			source:    "owner/repo.git",
			wantOwner: "owner",
			wantRepo:  "repo",
		},
		{
			name:      "HTTPS URL",
			source:    "https://github.com/owner/repo",
			wantOwner: "owner",
			wantRepo:  "repo",
		},
		{
			name:      "HTTPS URL with .git",
			source:    "https://github.com/owner/repo.git",
			wantOwner: "owner",
			wantRepo:  "repo",
		},
		{
			name:      "HTTP URL",
			source:    "http://github.com/owner/repo",
			wantOwner: "owner",
			wantRepo:  "repo",
		},
		{
			name:      "SSH URL",
			source:    "git@github.com:owner/repo.git",
			wantOwner: "owner",
			wantRepo:  "repo",
		},
		{
			name:      "SSH URL without .git",
			source:    "git@github.com:owner/repo",
			wantOwner: "owner",
			wantRepo:  "repo",
		},
		// Invalid formats
		{
			name:    "empty string",
			source:  "",
			wantErr: ErrInvalidSource,
		},
		{
			name:    "whitespace only",
			source:  "   ",
			wantErr: ErrInvalidSource,
		},
		{
			name:    "no separator",
			source:  "owner",
			wantErr: ErrInvalidSource,
		},
		{
			name:    "empty owner",
			source:  "/repo",
			wantErr: ErrInvalidSource,
		},
		{
			name:    "empty repo",
			source:  "owner/",
			wantErr: ErrInvalidSource,
		},
		{
			name:    "invalid URL - wrong domain",
			source:  "https://gitlab.com/owner/repo",
			wantErr: ErrInvalidSource,
		},
		{
			name:    "invalid URL - missing path",
			source:  "https://github.com/",
			wantErr: ErrInvalidSource,
		},
		{
			name:    "invalid SSH - wrong format",
			source:  "git@gitlab.com:owner/repo",
			wantErr: ErrInvalidSource,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotOwner, gotRepo, err := ParseSource(tt.source)

			if tt.wantErr != nil {
				if err == nil {
					t.Errorf("ParseSource() error = nil, wantErr %v", tt.wantErr)
					return
				}
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("ParseSource() error = %v, wantErr %v", err, tt.wantErr)
				}
				return
			}

			if err != nil {
				t.Errorf("ParseSource() unexpected error = %v", err)
				return
			}

			if gotOwner != tt.wantOwner {
				t.Errorf("ParseSource() owner = %v, want %v", gotOwner, tt.wantOwner)
			}
			if gotRepo != tt.wantRepo {
				t.Errorf("ParseSource() repo = %v, want %v", gotRepo, tt.wantRepo)
			}
		})
	}
}
