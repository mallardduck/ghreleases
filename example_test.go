package ghrelease_test

import (
	"context"
	"errors"
	"fmt"
	"io"

	ghrelease "github.com/mallardduck/ghreleases"
)

// rejectAll is a stand-in verifier. Real ones live in the sigstore and pgp
// sub-modules; any type with these two methods works.
type rejectAll struct{}

func (rejectAll) SignatureName(manifest string) string { return manifest + ".sig" }
func (rejectAll) Verify(context.Context, io.Reader, []byte) error {
	return errors.New("not signed by the expected party")
}

// Verification is opt-in. Without WithManifestVerifier nothing changes; with
// it, checksums are only returned from a manifest whose signature verified, and
// a missing or rejected signature is an error, never a silent fallback.
func ExampleWithManifestVerifier() {
	c := ghrelease.NewClient("", ghrelease.WithManifestVerifier(rejectAll{}))
	_ = c // c.FetchChecksums(ctx, url) now authenticates the manifest first

	// Callers can branch on the failure mode:
	for _, err := range []error{ghrelease.ErrSignatureMissing, ghrelease.ErrSignatureInvalid} {
		fmt.Println(err)
	}
	// Output:
	// signature not found
	// signature verification failed
}

// GitHub API asset URLs end in an opaque ID, so the signature cannot be found by
// name. Look it up in Release.Assets and pass it explicitly.
func ExampleWithSignatureURL() {
	ctx := ghrelease.WithSignatureURL(context.Background(),
		"https://api.github.com/repos/goreleaser/goreleaser/releases/assets/667103721")
	_ = ctx // pass to Client.FetchChecksums together with the manifest's asset URL
	fmt.Println("signature URL set")
	// Output: signature URL set
}

// Verify the asset itself, not just a checksum file next to it. The signature is
// fetched before the asset, and nothing reaches the destination until both the
// signature and the optional ExpectedHash have passed.
func ExampleWithArtifactVerifier() {
	c := ghrelease.NewClient("", ghrelease.WithArtifactVerifier(rejectAll{}))
	_ = c // c.Download(assetURL, w, ghrelease.DownloadOptions{SignatureURL: sigURL})
	fmt.Println(rejectAll{}.SignatureName("tool.tar.gz"))
	// Output: tool.tar.gz.sig
}
