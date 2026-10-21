package ghrelease

import (
	"errors"
	"testing"
)

func TestRender_ExtraVars(t *testing.T) {
	vars := TemplateVars{Name: "tool", Extra: map[string]string{"version_commit": "", "custom": "x", "name": "shadowed"}}

	got, err := Render("{custom}-{version_commit}-{name}", vars, TemplateStrict)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	// Empty-valued key is found; built-ins win over Extra.
	if want := "x--tool"; got != want {
		t.Errorf("Render() = %q, want %q", got, want)
	}

	if _, err := Render("{missing}", vars, TemplateStrict); !errors.Is(err, ErrUnknownVariable) {
		t.Errorf("Render({missing}) error = %v, want ErrUnknownVariable", err)
	}
}

func TestRender_StrictReturnsPartialResult(t *testing.T) {
	got, err := Render("a-{name}-{nope}-{os}", TemplateVars{Name: "n", OS: "linux"}, TemplateStrict)
	if !errors.Is(err, ErrUnknownVariable) {
		t.Fatalf("error = %v, want ErrUnknownVariable", err)
	}
	if want := "a-n-{nope}-linux"; got != want {
		t.Errorf("partial result = %q, want %q", got, want)
	}
}

func TestRender_Modifiers(t *testing.T) {
	vars := TemplateVars{
		Name: "kubectl", Version: "v1.30.1", OS: "linux", Arch: "amd64", Ext: "tar.gz",
		Extra: map[string]string{"version_commit": "", "tag": "release-1.2"},
	}
	tests := []struct {
		name    string
		pattern string
		want    string
		wantErr error
	}{
		{"plain text", "no tokens", "no tokens", nil},
		{"builtin vars", "{name}_{version}_{os}_{arch}.{ext}", "kubectl_v1.30.1_linux_amd64.tar.gz", nil},
		{"extra var", "{tag}", "release-1.2", nil},
		{"extra var empty value", "[{version_commit}]", "[]", nil},
		{"unknown variable", "{bogus}", "", ErrUnknownVariable},
		{"unknown modifier", "{os|bogus}", "", ErrUnknownModifier},
		{"upper", "{os|upper}", "LINUX", nil},
		{"lower", "{name|upper|lower}", "kubectl", nil},
		{"title", "{os|title}", "Linux", nil},
		{"trimprefix", "{version|trimprefix:v}", "1.30.1", nil},
		{"trimprefix no match", "{version|trimprefix:x}", "v1.30.1", nil},
		{"trimsuffix", "{ext|trimsuffix:.gz}", "tar", nil},
		{"trimsuffix match", "{ext|trimsuffix:gz}", "tar.", nil},
		{"replace exact match", "{os|replace:linux=Linux}", "Linux", nil},
		{"replace partial is not replaced", "{os|replace:lin=x}", "linux", nil},
		{"replace non-match", "{arch|replace:arm64=aarch64}", "amd64", nil},
		{"chained modifiers", "{version|trimprefix:v|replace:1.30.1=latest|upper}", "LATEST", nil},
		{"chained trim", "{version|trimprefix:v|trimsuffix:.1}", "1.30", nil},
		{"replace without FROM=TO", "{os|replace:linux}", "", ErrInvalidModifier},
		{"replace without argument", "{os|replace}", "", ErrInvalidModifier},
		// A modifier that requires an argument is an error, not a no-op;
		// this package reports ErrInvalidModifier.
		{"trimprefix without argument", "{version|trimprefix}", "", ErrInvalidModifier},
		{"trimsuffix without argument", "{version|trimsuffix}", "", ErrInvalidModifier},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Render(tt.pattern, vars, TemplateStrict)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("Render(%q) = %q, want %q", tt.pattern, got, tt.want)
			}
		})
	}
}
