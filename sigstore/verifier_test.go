package sigstore

import (
	"bytes"
	"context"
	"os"
	"testing"
)

const (
	testSAN    = `^https://github\.com/mallardduck/rancher-deployer/\.github/workflows/release\.yml@refs/tags/v.+$`
	testIssuer = "https://token.actions.githubusercontent.com"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func newTestVerifier(t *testing.T, id Identity) *Verifier {
	t.Helper()
	v, err := NewVerifier(id, WithTrustedRoot(fixture(t, "trusted_root.json")))
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	return v
}

func TestVerifier_Verify(t *testing.T) {
	manifest := fixture(t, "checksums.txt")
	bundle := fixture(t, "checksums.txt.sigstore.json")
	good := Identity{SubjectRegexp: testSAN, Issuer: testIssuer}

	tests := []struct {
		name     string
		id       Identity
		manifest []byte
		bundle   []byte
		wantErr  bool
	}{
		{"genuine release", good, manifest, bundle, false},
		{"tampered manifest", good, append(append([]byte{}, manifest...), []byte("deadbeef  evil\n")...), bundle, true},
		{"wrong workflow identity", Identity{SubjectRegexp: `^https://github\.com/someone-else/repo/.*$`, Issuer: testIssuer}, manifest, bundle, true},
		{"wrong issuer", Identity{SubjectRegexp: testSAN, Issuer: "https://accounts.google.com"}, manifest, bundle, true},
		{"garbage bundle", good, manifest, []byte("not a bundle"), true},
		{"empty bundle", good, manifest, nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := newTestVerifier(t, tt.id)
			err := v.Verify(context.Background(), bytes.NewReader(tt.manifest), tt.bundle)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Verify() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestNewVerifier_RequiresPinnedIdentity(t *testing.T) {
	root := WithTrustedRoot(fixture(t, "trusted_root.json"))
	for _, id := range []Identity{
		{},
		{SubjectRegexp: testSAN},
		{Issuer: testIssuer},
		{SubjectRegexp: "(", Issuer: testIssuer},
	} {
		if _, err := NewVerifier(id, root); err == nil {
			t.Errorf("NewVerifier(%+v) = nil error, want rejection of an unpinned or invalid identity", id)
		}
	}
}

func TestVerifier_SignatureName(t *testing.T) {
	v := newTestVerifier(t, Identity{SubjectRegexp: testSAN, Issuer: testIssuer})
	if got := v.SignatureName("checksums.txt"); got != "checksums.txt.sigstore.json" {
		t.Errorf("SignatureName() = %q", got)
	}
}
