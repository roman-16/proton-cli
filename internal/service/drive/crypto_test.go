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
