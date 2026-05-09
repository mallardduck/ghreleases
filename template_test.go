package ghrelease

import (
	"errors"
	"testing"
)

func TestRender(t *testing.T) {
	vars := TemplateVars{
		Name:    "myapp",
		Version: "v1.0.0",
		OS:      "linux",
		Arch:    "amd64",
		Ext:     "tar.gz",
	}

	tests := []struct {
		name    string
		pattern string
		vars    TemplateVars
		mode    TemplateMode
		want    string
		wantErr error
	}{
		// Basic variables
		{
			name:    "single variable",
			pattern: "{name}",
			vars:    vars,
			mode:    TemplatePermissive,
			want:    "myapp",
		},
		{
			name:    "multiple variables",
			pattern: "{name}_{version}_{os}_{arch}.{ext}",
			vars:    vars,
			mode:    TemplatePermissive,
			want:    "myapp_v1.0.0_linux_amd64.tar.gz",
		},
		{
			name:    "variable with literal text",
			pattern: "download-{name}-{version}.zip",
			vars:    vars,
			mode:    TemplatePermissive,
			want:    "download-myapp-v1.0.0.zip",
		},
		// Simple modifiers
		{
			name:    "upper modifier",
			pattern: "{name|upper}",
			vars:    vars,
			mode:    TemplatePermissive,
			want:    "MYAPP",
		},
		{
			name:    "lower modifier",
			pattern: "{os|lower}",
			vars:    TemplateVars{OS: "LINUX"},
			mode:    TemplatePermissive,
			want:    "linux",
		},
		{
			name:    "title modifier",
			pattern: "{name|title}",
			vars:    vars,
			mode:    TemplatePermissive,
			want:    "Myapp",
		},
		// Argument modifiers
		{
			name:    "trimprefix modifier",
			pattern: "{version|trimprefix:v}",
			vars:    vars,
			mode:    TemplatePermissive,
			want:    "1.0.0",
		},
		{
			name:    "trimsuffix modifier",
			pattern: "{ext|trimsuffix:.gz}",
			vars:    vars,
			mode:    TemplatePermissive,
			want:    "tar",
		},
		{
			name:    "replace modifier",
			pattern: "{os|replace:linux=ubuntu}",
			vars:    vars,
			mode:    TemplatePermissive,
			want:    "ubuntu",
		},
		{
			name:    "replace modifier darwin to macos",
			pattern: "{os|replace:darwin=macos}",
			vars:    TemplateVars{OS: "darwin"},
			mode:    TemplatePermissive,
			want:    "macos",
		},
		{
			name:    "replace modifier no match",
			pattern: "{os|replace:darwin=macos}",
			vars:    vars,
			mode:    TemplatePermissive,
			want:    "linux", // no match, value unchanged
		},
		// Complex patterns
		{
			name:    "multiple modifiers in pattern",
			pattern: "{name|upper}-{version|trimprefix:v}-{os}.{ext}",
			vars:    vars,
			mode:    TemplatePermissive,
			want:    "MYAPP-1.0.0-linux.tar.gz",
		},
		{
			name:    "chained modifiers",
			pattern: "{os|replace:darwin=macos|upper}",
			vars:    TemplateVars{OS: "darwin"},
			mode:    TemplatePermissive,
			want:    "MACOS",
		},
		{
			name:    "multiple chained modifiers",
			pattern: "{version|trimprefix:v|replace:1.0.0=1.0.1}",
			vars:    vars,
			mode:    TemplatePermissive,
			want:    "1.0.1",
		},
		// Permissive mode - unknown variable
		{
			name:    "permissive mode - unknown variable",
			pattern: "{unknown}-{name}",
			vars:    vars,
			mode:    TemplatePermissive,
			want:    "{unknown}-myapp",
			wantErr: nil,
		},
		// Permissive mode - unknown modifier
		{
			name:    "permissive mode - unknown modifier",
			pattern: "{name|unknown}",
			vars:    vars,
			mode:    TemplatePermissive,
			want:    "{name|unknown}",
			wantErr: nil,
		},
		// Strict mode - unknown variable
		{
			name:    "strict mode - unknown variable",
			pattern: "{unknown}",
			vars:    vars,
			mode:    TemplateStrict,
			wantErr: ErrUnknownVariable,
		},
		// Strict mode - unknown modifier
		{
			name:    "strict mode - unknown modifier",
			pattern: "{name|unknown}",
			vars:    vars,
			mode:    TemplateStrict,
			wantErr: ErrUnknownModifier,
		},
		// Error cases
		{
			name:    "invalid replace syntax - no equals",
			pattern: "{name|replace:invalid}",
			vars:    vars,
			mode:    TemplateStrict,
			wantErr: ErrInvalidModifier,
		},
		{
			name:    "invalid replace in permissive",
			pattern: "{name|replace:invalid}",
			vars:    vars,
			mode:    TemplatePermissive,
			want:    "{name|replace:invalid}",
			wantErr: nil,
		},
		// No variables - plain text
		{
			name:    "no variables",
			pattern: "plain-text-file.tar.gz",
			vars:    vars,
			mode:    TemplatePermissive,
			want:    "plain-text-file.tar.gz",
		},
		// Empty variable values
		{
			name:    "empty variable value",
			pattern: "{name}-{version}",
			vars:    TemplateVars{Name: "app", Version: ""},
			mode:    TemplatePermissive,
			want:    "app-",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Render(tt.pattern, tt.vars, tt.mode)

			if tt.wantErr != nil {
				if err == nil {
					t.Errorf("Render() error = nil, wantErr %v", tt.wantErr)
					return
				}
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("Render() error = %v, wantErr %v", err, tt.wantErr)
				}
				return
			}

			if err != nil {
				t.Errorf("Render() unexpected error = %v", err)
				return
			}

			if got != tt.want {
				t.Errorf("Render() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLookupVar(t *testing.T) {
	vars := TemplateVars{
		Name:    "test",
		Version: "1.0",
		OS:      "linux",
		Arch:    "amd64",
		Ext:     "tar.gz",
	}

	tests := []struct {
		name      string
		varName   string
		wantValue string
		wantFound bool
	}{
		{"name", "name", "test", true},
		{"version", "version", "1.0", true},
		{"os", "os", "linux", true},
		{"arch", "arch", "amd64", true},
		{"ext", "ext", "tar.gz", true},
		{"unknown", "unknown", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotValue, gotFound := lookupVar(vars, tt.varName)
			if gotValue != tt.wantValue {
				t.Errorf("lookupVar() value = %v, want %v", gotValue, tt.wantValue)
			}
			if gotFound != tt.wantFound {
				t.Errorf("lookupVar() found = %v, want %v", gotFound, tt.wantFound)
			}
		})
	}
}

func TestApplyModifier(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		modifier string
		want     string
		wantErr  error
	}{
		// Simple modifiers
		{"upper", "test", "upper", "TEST", nil},
		{"lower", "TEST", "lower", "test", nil},
		{"title", "test", "title", "Test", nil},
		// Prefix/suffix modifiers
		{"trimprefix", "v1.0.0", "trimprefix:v", "1.0.0", nil},
		{"trimsuffix", "file.tar.gz", "trimsuffix:.gz", "file.tar", nil},
		{"trimprefix no match", "1.0.0", "trimprefix:v", "1.0.0", nil},
		// Replace modifier (exact match only, not ReplaceAll)
		{"replace exact match", "darwin", "replace:darwin=macos", "macos", nil},
		{"replace no match", "linux", "replace:darwin=macos", "linux", nil},
		// Error cases
		{"unknown modifier", "test", "unknown", "", ErrUnknownModifier},
		{"invalid replace", "test", "replace:noequals", "", ErrInvalidModifier},
		{"unknown with arg", "test", "badmod:arg", "", ErrUnknownModifier},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := applyModifier(tt.value, tt.modifier)

			if tt.wantErr != nil {
				if err == nil {
					t.Errorf("applyModifier() error = nil, wantErr %v", tt.wantErr)
					return
				}
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("applyModifier() error = %v, wantErr %v", err, tt.wantErr)
				}
				return
			}

			if err != nil {
				t.Errorf("applyModifier() unexpected error = %v", err)
				return
			}

			if got != tt.want {
				t.Errorf("applyModifier() = %v, want %v", got, tt.want)
			}
		})
	}
}
