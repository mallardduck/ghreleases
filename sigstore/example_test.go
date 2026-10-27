package sigstore_test

import (
	"fmt"
	"log"

	"github.com/mallardduck/ghreleases/sigstore"
)

// Pin the exact workflow that signs your releases. The result implements
// ghrelease.ManifestVerifier; pass it to ghrelease.WithManifestVerifier.
func ExampleNewVerifier() {
	v, err := sigstore.NewVerifier(sigstore.Identity{
		SubjectRegexp: `^https://github\.com/goreleaser/goreleaser/\.github/workflows/release\.yml@refs/tags/v.+$`,
		Issuer:        "https://token.actions.githubusercontent.com",
	})
	if err != nil {
		log.Fatal(err)
	}

	// The client locates the bundle by this name next to checksums.txt.
	fmt.Println(v.SignatureName("checksums.txt"))
	// Output: checksums.txt.sigstore.json
}

// Some projects name the bundle differently; yq publishes "checksums.bundle".
func ExampleWithBundleSuffix() {
	v, err := sigstore.NewVerifier(sigstore.Identity{
		SubjectRegexp: `^https://github\.com/mikefarah/yq/\.github/workflows/release\.yml@refs/tags/v.+$`,
		Issuer:        "https://token.actions.githubusercontent.com",
	}, sigstore.WithBundleSuffix(".bundle"))
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(v.SignatureName("checksums"))
	// Output: checksums.bundle
}

// A permissive pattern is rejected by default, because a valid signature from
// an arbitrary identity proves nothing. Opting in is explicit.
func ExampleWithInsecureSubjectPattern() {
	id := sigstore.Identity{SubjectRegexp: `.*`, Issuer: "https://token.actions.githubusercontent.com"}

	_, err := sigstore.NewVerifier(id)
	fmt.Println("default rejects:", err != nil)

	_, err = sigstore.NewVerifier(id, sigstore.WithInsecureSubjectPattern())
	fmt.Println("opt-in accepts:", err == nil)
	// Output:
	// default rejects: true
	// opt-in accepts: true
}
