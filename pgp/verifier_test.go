package pgp

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
)

// Helm v4.3.1 is signed by a maintainer key listed in helm's KEYS file, which
// concatenates several armored public key blocks. These are the genuine files.
const (
	helmSigner = "208DD36ED5BB3745A16743A4C7C6FBB5B91C1155" // Scott Rigby
	// helmOther is a different maintainer in KEYS who did not sign v4.3.1.
	helmOther = "967F8AC5E2216F9F4FD270AD92AA783CBAAE8E3B"

	linuxSum  = "helm-v4.3.1-linux-amd64.tar.gz.sha256sum"
	linuxHash = "helm-v4.3.1-linux-amd64.tar.gz.sha256"
	darwinSum = "helm-v4.3.1-darwin-arm64.tar.gz.sha256sum"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "helm-v4.3.1", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func helmVerifier(t *testing.T, opts ...Option) *Verifier {
	t.Helper()
	v, err := NewVerifier(fixture(t, "KEYS"), opts...)
	if err != nil {
		t.Fatalf("NewVerifier(helm KEYS): %v", err)
	}
	return v
}

func TestVerifier_GenuineHelmFiles(t *testing.T) {
	v := helmVerifier(t)
	for _, name := range []string{linuxSum, linuxHash, darwinSum} {
		t.Run(name, func(t *testing.T) {
			if err := v.Verify(context.Background(), fixture(t, name), fixture(t, name+".asc")); err != nil {
				t.Fatalf("genuine file rejected: %v", err)
			}
		})
	}
}

func TestVerifier_GenuineWithFingerprintPin(t *testing.T) {
	v := helmVerifier(t, WithFingerprints(helmSigner))
	if err := v.Verify(context.Background(), fixture(t, linuxSum), fixture(t, linuxSum+".asc")); err != nil {
		t.Fatalf("genuine file rejected under its signer's pin: %v", err)
	}
	// Fingerprints are case- and space-insensitive, as printed by gpg.
	v = helmVerifier(t, WithFingerprints(strings.ToLower("208D D36E D5BB 3745 A167  43A4 C7C6 FBB5 B91C 1155")))
	if err := v.Verify(context.Background(), fixture(t, linuxSum), fixture(t, linuxSum+".asc")); err != nil {
		t.Fatalf("gpg-formatted fingerprint pin rejected: %v", err)
	}
}

func TestVerifier_RejectsBadInput(t *testing.T) {
	good := fixture(t, linuxSum)
	sig := fixture(t, linuxSum+".asc")
	swapped := bytes.Replace(good, []byte("0403a88a"), []byte("deadbeef"), 1)

	tests := []struct {
		name     string
		manifest []byte
		sig      []byte
	}{
		{"tampered hash", swapped, sig},
		{"appended line", append(append([]byte{}, good...), []byte("00  evil.tar.gz\n")...), sig},
		{"truncated manifest", good[:len(good)-1], sig},
		{"signature replayed from another file", good, fixture(t, darwinSum+".asc")},
		{"signature for a different manifest", fixture(t, darwinSum), sig},
		{"garbage signature", good, []byte("not a signature")},
		{"empty signature", good, nil},
		{"truncated armor", good, sig[:len(sig)/2]},
		{"empty manifest", nil, sig},
	}
	v := helmVerifier(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := v.Verify(context.Background(), tt.manifest, tt.sig); err == nil {
				t.Fatal("Verify() = nil, want rejection")
			}
		})
	}
}

func TestVerifier_AcceptsBinarySignature(t *testing.T) {
	blk, err := armor.Decode(bytes.NewReader(fixture(t, linuxSum+".asc")))
	if err != nil {
		t.Fatal(err)
	}
	bin, err := io.ReadAll(blk.Body)
	if err != nil {
		t.Fatal(err)
	}
	if err := helmVerifier(t).Verify(context.Background(), fixture(t, linuxSum), bin); err != nil {
		t.Fatalf("binary detached signature rejected: %v", err)
	}
}

// keyringWithout re-exports helm's KEYS minus the key with the given primary
// fingerprint.
func keyringWithout(t *testing.T, fp string) []byte {
	t.Helper()
	var all openpgp.EntityList
	rest := fixture(t, "KEYS")
	for {
		i := bytes.Index(rest, []byte("-----BEGIN PGP PUBLIC KEY BLOCK-----"))
		if i < 0 {
			break
		}
		rest = rest[i:]
		list, err := openpgp.ReadArmoredKeyRing(bytes.NewReader(rest))
		if err != nil {
			t.Fatal(err)
		}
		all = append(all, list...)
		j := bytes.Index(rest, []byte("-----END PGP PUBLIC KEY BLOCK-----"))
		rest = rest[j+1:]
	}
	var out bytes.Buffer
	n := 0
	for _, e := range all {
		if strings.EqualFold(strings.ToUpper(hexFP(e)), fp) {
			continue
		}
		if err := e.Serialize(&out); err != nil {
			t.Fatal(err)
		}
		n++
	}
	if n == 0 {
		t.Fatal("empty keyring")
	}
	return out.Bytes()
}

func hexFP(e *openpgp.Entity) string {
	var sb strings.Builder
	for _, b := range e.PrimaryKey.Fingerprint {
		sb.WriteString(strings.ToUpper(string("0123456789abcdef"[b>>4])) + strings.ToUpper(string("0123456789abcdef"[b&0xf])))
	}
	return sb.String()
}

func TestVerifier_SignerNotTrusted(t *testing.T) {
	good, sig := fixture(t, linuxSum), fixture(t, linuxSum+".asc")

	t.Run("signer absent from keyring", func(t *testing.T) {
		v, err := NewVerifier(keyringWithout(t, helmSigner))
		if err != nil {
			t.Fatal(err)
		}
		if err := v.Verify(context.Background(), good, sig); err == nil {
			t.Fatal("signature by a key outside the keyring was accepted")
		}
	})
	t.Run("fingerprint pin names a different key", func(t *testing.T) {
		v := helmVerifier(t, WithFingerprints(helmOther))
		if err := v.Verify(context.Background(), good, sig); err == nil {
			t.Fatal("signature accepted though the pin excludes its signer")
		}
	})
}

func TestNewVerifier_RequiresKeys(t *testing.T) {
	for name, kr := range map[string][]byte{
		"nil keyring":          nil,
		"empty keyring":        {},
		"garbage keyring":      []byte("not a keyring"),
		"signature, not a key": fixture(t, linuxSum+".asc"),
	} {
		if _, err := NewVerifier(kr); err == nil {
			t.Errorf("NewVerifier(%s) = nil error, want refusal: a keyring with no keys proves nothing", name)
		}
	}
}

func TestNewVerifier_RejectsUnsatisfiablePins(t *testing.T) {
	kr := fixture(t, "KEYS")
	for name, fps := range map[string][]string{
		"not hex":         {"zzzz"},
		"too short":       {"208DD36E"},
		"not in keyring":  {strings.Repeat("A", 40)},
		"empty list":      {},
		"one bad of many": {helmSigner, "nope"},
	} {
		if _, err := NewVerifier(kr, WithFingerprints(fps...)); err == nil {
			t.Errorf("NewVerifier(pin %s) = nil error: a pin that can never match must fail loudly, not silently reject every signature later", name)
		}
	}
}

func TestVerifier_SignatureName(t *testing.T) {
	if got := helmVerifier(t).SignatureName(linuxSum); got != linuxSum+".asc" {
		t.Errorf("SignatureName() = %q", got)
	}
	if got := helmVerifier(t, WithSignatureSuffix(".sig")).SignatureName("checksums.txt"); got != "checksums.txt.sig" {
		t.Errorf("custom suffix SignatureName() = %q", got)
	}
}

// An empty suffix would make the signature URL equal the manifest URL.
func TestNewVerifier_RejectsEmptySignatureSuffix(t *testing.T) {
	if _, err := NewVerifier(fixture(t, "KEYS"), WithSignatureSuffix("")); err == nil {
		t.Fatal("empty signature suffix accepted")
	}
}
