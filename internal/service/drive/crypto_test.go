package drive

import (
	"strings"
	"testing"
	"time"

	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
)

// A file's record of itself is sealed to its own node key, so what a client
// writes there is what every other client - and a later download - reads back.
func TestAFilesRecordOfItselfRoundTrips(t *testing.T) {
	key, err := pgp.GenerateKey("Node", "", "x25519", 0)
	if err != nil {
		t.Fatalf("generate a node key: %v", err)
	}
	nodeKR, err := pgp.NewKeyRing(key)
	if err != nil {
		t.Fatalf("node key ring: %v", err)
	}
	up := &uploaded{blockSizes: []int{driveBlockSize, 17}, size: driveBlockSize + 17, sha1: "0a1b2c"}
	modified := time.Date(2026, 4, 15, 14, 32, 8, 0, time.UTC)

	armored, err := encryptXAttr(up.record(modified), nodeKR, nodeKR)
	if err != nil {
		t.Fatalf("encryptXAttr: %v", err)
	}
	x, err := decryptXAttr(armored, nodeKR)
	if err != nil {
		t.Fatalf("decryptXAttr: %v", err)
	}
	if x.Common.Size != up.size {
		t.Errorf("Size = %d, want %d", x.Common.Size, up.size)
	}
	if x.Common.Digests.SHA1 != up.sha1 {
		t.Errorf("SHA1 = %q, want %q", x.Common.Digests.SHA1, up.sha1)
	}
	if len(x.Common.BlockSizes) != 2 || x.Common.BlockSizes[1] != 17 {
		t.Errorf("BlockSizes = %v, want the size of each block in order", x.Common.BlockSizes)
	}
	if x.Common.ModificationTime != "2026-04-15T14:32:08.000Z" {
		t.Errorf("ModificationTime = %q, want the form every Proton client reads",
			x.Common.ModificationTime)
	}
}

// A stream was not a file anywhere, so it has no modification time to record -
// and the record says nothing rather than claiming a moment.
func TestAStreamsRecordClaimsNoModificationTime(t *testing.T) {
	up := &uploaded{size: 9, sha1: "0a1b2c"}
	if got := up.record(time.Time{}); got.Common.ModificationTime != "" {
		t.Errorf("ModificationTime = %q, want none", got.Common.ModificationTime)
	}
	if !strings.Contains(object(t, up.record(time.Time{})), `"Size":9`) {
		t.Error("the record should still say how many bytes there were")
	}
}

// A name is hashed exactly as it is written, the way Proton's own clients hash
// it, so a name the CLI writes is found by theirs and theirs by the CLI. Two
// names that differ only in case are two names.
func TestANameIsHashedExactlyAsWritten(t *testing.T) {
	key := []byte("photo-library-hash-key-32-bytes!")
	for name, want := range map[string]string{
		"IMG_0001.JPG": "863178b65bc60610b4e8c01b3a6aa01dde5dece9b83a83c069f4cd506c79bf70",
		"img_0001.jpg": "0bbeecc18a5ecb154cec1c1902670a040f02fce442dafbc29c9e5378feee8c5a",
	} {
		got, err := lookupHash(name, key)
		if err != nil {
			t.Fatalf("lookupHash(%q): %v", name, err)
		}
		if got != want {
			t.Errorf("lookupHash(%q) = %s, want %s", name, got, want)
		}
	}
}
