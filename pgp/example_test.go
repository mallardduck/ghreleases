package pgp_test

import (
	"fmt"
	"log"
	"os"

	"github.com/mallardduck/ghreleases/pgp"
)

// Trust the keys in a project's KEYS file. The result implements
// ghrelease.ManifestVerifier; pass it to ghrelease.WithManifestVerifier.
func ExampleNewVerifier() {
	keys, err := os.ReadFile("testdata/helm-v4.3.1/KEYS")
	if err != nil {
		log.Fatal(err)
	}
	v, err := pgp.NewVerifier(keys)
	if err != nil {
		log.Fatal(err)
	}

	// The client locates the signature by this name next to the checksum file.
	fmt.Println(v.SignatureName("helm-v4.3.1-linux-amd64.tar.gz.sha256sum"))
	// Output: helm-v4.3.1-linux-amd64.tar.gz.sha256sum.asc
}

// Narrow trust from "any key in KEYS" to the maintainer who cut the release.
func ExampleWithFingerprints() {
	keys, err := os.ReadFile("testdata/helm-v4.3.1/KEYS")
	if err != nil {
		log.Fatal(err)
	}
	_, err = pgp.NewVerifier(keys, pgp.WithFingerprints("208D D36E D5BB 3745 A167  43A4 C7C6 FBB5 B91C 1155"))
	fmt.Println("pinned:", err == nil)

	// A pin that names a key absent from the keyring fails immediately.
	_, err = pgp.NewVerifier(keys, pgp.WithFingerprints("0000000000000000000000000000000000000000"))
	fmt.Println("unknown key rejected:", err != nil)
	// Output:
	// pinned: true
	// unknown key rejected: true
}
