package drive

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"github.com/roman-16/proton-cli/internal/account/keys"
	"strings"
	"testing"
)

func hashOf(b []byte) string {
	sum := sha256.Sum256(b)
	return base64.StdEncoding.EncodeToString(sum[:])
}

func blocksFor(parts ...[]byte) []revisionBlock {
	out := make([]revisionBlock, 0, len(parts))
	for i, p := range parts {
		out = append(out, revisionBlock{Index: i + 1, Hash: hashOf(p)})
	}
	return out
}

// The revision signature covers the thumbnail hashes followed by the block hashes,
// in order. Rebuilding it in any other shape verifies a different file.
func TestManifestIsThumbnailHashesThenBlockHashesInOrder(t *testing.T) {
	first, second := []byte("first"), []byte("second")
	thumb := []byte("thumb")
	rev := revision{
		Thumbnails: []revisionThumbnail{{Hash: hashOf(thumb)}},
		Blocks:     blocksFor(first, second),
	}
	manifest, hashes, err := rev.manifest()
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest) != 3*sha256.Size {
		t.Fatalf("manifest is %d bytes, want three hashes", len(manifest))
	}
	want := decoded(t, hashOf(thumb)) + decoded(t, hashOf(first)) + decoded(t, hashOf(second))
	if string(manifest) != want {
		t.Error("the manifest is not the thumbnail hash followed by the block hashes")
	}
	if len(hashes) != 2 || string(hashes[0]) != decoded(t, hashOf(first)) {
		t.Error("the per-block hashes do not match the blocks")
	}
}

func decoded(t *testing.T, encoded string) string {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// A block list with a gap, a repeat or a wrong start describes different content
// from the file about to be written, so the manifest cannot be built from it.
func TestManifestRefusesABlockListThatIsNotContiguousFromOne(t *testing.T) {
	body := []byte("x")
	for _, tc := range []struct {
		name    string
		indexes []int
	}{
		{"starting at two", []int{2, 3}},
		{"with a gap", []int{1, 3}},
		{"with a repeat", []int{1, 1}},
		{"out of order", []int{2, 1}},
	} {
		blocks := make([]revisionBlock, 0, len(tc.indexes))
		for _, i := range tc.indexes {
			blocks = append(blocks, revisionBlock{Index: i, Hash: hashOf(body)})
		}
		if _, _, err := (revision{Blocks: blocks}).manifest(); err == nil {
			t.Errorf("a block list %s was accepted", tc.name)
		}
	}
}

func TestManifestRefusesAHashItCannotUse(t *testing.T) {
	for _, tc := range []struct{ name, hash string }{
		{"not base64", "not base64!!"},
		{"the wrong length", base64.StdEncoding.EncodeToString([]byte("short"))},
		{"absent", ""},
	} {
		rev := revision{Blocks: []revisionBlock{{Index: 1, Hash: tc.hash}}}
		if _, _, err := rev.manifest(); err == nil {
			t.Errorf("a block hash that is %s was accepted", tc.name)
		}
	}
	rev := revision{
		Thumbnails: []revisionThumbnail{{Hash: "not base64!!"}},
		Blocks:     blocksFor([]byte("x")),
	}
	if _, _, err := rev.manifest(); err == nil {
		t.Error("a thumbnail hash that is not base64 was accepted")
	}
}

// A revision with no signature cannot be checked, and content that cannot be
// checked is content that must not be written.
func TestVerifyManifestRefusesARevisionWithNoSignature(t *testing.T) {
	err := (&Service{}).verifyManifest(context.Background(), &Context{}, nil, "", []byte("manifest"), "")
	if err == nil {
		t.Fatal("a revision with no manifest signature was accepted")
	}
	if !strings.Contains(err.Error(), "no manifest signature") {
		t.Errorf("the refusal does not say why: %v", err)
	}
}

// Behind a public link there is no signature to check, because the reader is
// outside the share the signing key belongs to. Refusing the file over that
// would refuse every file a link can offer.
func TestVerifyManifestAcceptsAPublicLinkWithNoSignature(t *testing.T) {
	err := (&Service{}).verifyManifest(context.Background(), &Context{Token: "7X2K9M3N1P"}, nil, "", []byte("manifest"), "")
	if err != nil {
		t.Errorf("a public link's content was refused for want of a signature: %v", err)
	}
}

// Older revisions name the committer in SignatureAddress, newer ones in
// SignatureEmail. Verifying against the wrong one fails a good file.
func TestAuthorPrefersTheNewerField(t *testing.T) {
	both := revision{SignatureAddress: "old@proton.me", SignatureEmail: "new@proton.me"}
	if got := both.author(); got != "new@proton.me" {
		t.Errorf("author = %q", got)
	}
	onlyOld := revision{SignatureAddress: "old@proton.me"}
	if got := onlyOld.author(); got != "old@proton.me" {
		t.Errorf("author = %q", got)
	}
	// Anonymous content names nobody, and is verified against the node key.
	if got := (revision{}).author(); got != "" {
		t.Errorf("author = %q, want nobody", got)
	}
}

// testKeys hands a service the key hierarchy a test wants it to decrypt with.
// A test that decrypts nothing passes nil, which is never asked for.
func testKeys(u *keys.Unlocked) keys.Get {
	return func(context.Context) (*keys.Unlocked, error) {
		if u == nil {
			return nil, errors.New("this test has no keys")
		}
		return u, nil
	}
}
