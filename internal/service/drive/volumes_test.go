package drive

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
	"github.com/roman-16/proton-cli/internal/account/keys"
	"github.com/roman-16/proton-cli/internal/proton"
)

// A password reset locks the volume the files are on and Drive starts again on a
// fresh one. None of that can be reached by a live test - no test account may be
// reset - so which volume the commands work in, and what a restore hands Proton,
// are pinned here.

const (
	activeVolume = `{"VolumeID":"active","State":1,"Type":1,"Share":{"ShareID":"active-share","LinkID":"root"}}`
	lockedVolume = `{"VolumeID":"old","State":3,"Type":1,"UsedSpace":41401599,"CreateTime":1676134920,` +
		`"Share":{"ShareID":"old-share","LinkID":"old-root"}}`
)

func volumeList(bodies ...string) string {
	return `{"Volumes":[` + strings.Join(bodies, ",") + `]}`
}

// Every command works in the account's active files volume. Taking whichever
// volume comes first is what breaks Drive after a password reset, and it would
// show the tree from before the reset as the one in use.
func TestDriveWorksInTheActiveVolume(t *testing.T) {
	doer := &stubDoer{routes: map[string]string{
		"GET /drive/volumes": volumeList(lockedVolume, activeVolume),
		"GET /drive/shares/active-share": `{"Share":{"ShareID":"active-share","AddressID":"addr",` +
			`"Key":"","Passphrase":"","LinkID":"root"}}`,
	}}
	_, err := New(doer, testKeys(signedInAs(t, "me@proton.me"))).Resolve(context.Background())
	// The share cannot be unwrapped by a stub, but which share was asked for is
	// the whole of what this decides.
	if !doer.sent("GET", "/drive/shares/active-share") {
		t.Fatalf("asked for %v, want the active volume's share; err = %v", doer.reqs, err)
	}
	if doer.sent("GET", "/drive/shares/old-share") {
		t.Error("a volume a password reset locked was opened as the one in use")
	}
}

// An account with no active files volume has nothing to open, and says so as
// something a caller can recognise rather than as a decryption failure.
func TestDriveSaysWhenThereIsNoVolume(t *testing.T) {
	for _, tc := range []struct{ name, volumes string }{
		{name: "a brand-new account", volumes: volumeList()},
		{name: "every volume locked", volumes: volumeList(lockedVolume)},
	} {
		doer := &stubDoer{routes: map[string]string{"GET /drive/volumes": tc.volumes}}
		_, err := New(doer, testKeys(signedInAs(t, "me@proton.me"))).Resolve(context.Background())
		var none *NoVolume
		if !errors.As(err, &none) {
			t.Errorf("%s: err = %v, want NoVolume", tc.name, err)
		}
	}
}

// The listing reads what a volume is and what state it is in, and finds a locked
// volume that the volume listing itself left out - the locked shares name it.
func TestVolumesListsWhatALockedShareNames(t *testing.T) {
	doer := &stubDoer{routes: map[string]string{
		"GET /drive/volumes": volumeList(activeVolume),
		"GET /drive/shares": `{"Shares":[{"ShareID":"old-share","VolumeID":"old","Type":1,"Locked":true},` +
			`{"ShareID":"active-share","VolumeID":"active","Type":1}]}`,
		"GET /drive/volumes/old": `{"Volume":` + lockedVolume + `}`,
	}}
	volumes, err := New(doer, testKeys(nil)).Volumes(context.Background())
	if err != nil {
		t.Fatalf("Volumes: %v", err)
	}
	if len(volumes) != 2 {
		t.Fatalf("listed %d volumes, want the active one and the locked one", len(volumes))
	}
	active, old := volumes[0], volumes[1]
	if active.ID != "active" || active.State != "active" || active.Type != "files" || active.Locked() {
		t.Errorf("the active volume reads as %+v", active)
	}
	if old.ID != "old" || !old.Locked() || old.UsedSpace != 41401599 || old.Created != 1676134920 {
		t.Errorf("the locked volume reads as %+v", old)
	}
	if old.Restore != "" {
		t.Errorf("restore = %q, want nothing said about a restore nobody asked for", old.Restore)
	}
}

// A restore under way is reported, so somebody who ran one yesterday can see
// where it got to.
func TestVolumesReportsARestoreUnderWay(t *testing.T) {
	doer := &stubDoer{routes: map[string]string{
		"GET /drive/volumes": volumeList(
			`{"VolumeID":"old","State":3,"Type":2,"RestoreStatus":1}`,
			`{"VolumeID":"failed","State":3,"Type":1,"RestoreStatus":-1}`,
			`{"VolumeID":"done","State":1,"Type":1,"RestoreStatus":0}`),
		"GET /drive/shares": `{"Shares":[]}`,
	}}
	volumes, err := New(doer, testKeys(nil)).Volumes(context.Background())
	if err != nil {
		t.Fatalf("Volumes: %v", err)
	}
	want := []struct{ typ, restore string }{{"photos", "in progress"}, {"files", "failed"}, {"files", "done"}}
	for i, w := range want {
		if volumes[i].Type != w.typ || volumes[i].Restore != w.restore {
			t.Errorf("volume %d reads as %s/%q, want %s/%q", i, volumes[i].Type, volumes[i].Restore, w.typ, w.restore)
		}
	}
}

// ── restoring ──

// lockedTree is a volume a password reset locked, as Proton hands it back: the
// share's passphrase sealed to an address key of the account's, and the root's
// passphrase sealed to the share key.
type lockedTree struct {
	shareID string
	kind    int
	body    string
	// rootPassphrase is what a restore has to hand back, re-sealed.
	rootPassphrase string
	sessionKey     *pgp.SessionKey
}

func lockLikeAReset(t *testing.T, u *keys.Unlocked, shareID string, kind int) *lockedTree {
	t.Helper()
	rings, ok := u.AddrRings(testAddrID)
	if !ok {
		t.Fatal("the test account holds no address keys")
	}

	shareKey, sharePass, _, sharePriv, err := genNodeKeys(rings.Write, rings.Write)
	if err != nil {
		t.Fatalf("make the locked share's key: %v", err)
	}
	msg, err := pgp.NewPGPMessageFromArmored(sharePass)
	if err != nil {
		t.Fatalf("read the share passphrase: %v", err)
	}
	split, err := msg.SplitMessage()
	if err != nil {
		t.Fatalf("split the share passphrase: %v", err)
	}
	sessionKey, err := rings.Read.DecryptSessionKey(split.GetBinaryKeyPacket())
	if err != nil {
		t.Fatalf("read the share's session key: %v", err)
	}
	plain, err := sessionKey.Decrypt(split.GetBinaryDataPacket())
	if err != nil {
		t.Fatalf("open the share passphrase: %v", err)
	}
	signature, err := rings.Write.SignDetached(pgp.NewPlainMessageFromString(plain.GetString()))
	if err != nil {
		t.Fatalf("sign the share passphrase: %v", err)
	}
	armoredSig, err := signature.GetArmored()
	if err != nil {
		t.Fatalf("armor the signature: %v", err)
	}

	shareKR, err := pgp.NewKeyRing(sharePriv)
	if err != nil {
		t.Fatalf("share key ring: %v", err)
	}
	rootPassphrase := "the root passphrase from before the reset"
	sealedRoot, _, err := sealPassphrase(rootPassphrase, shareKR, shareKR)
	if err != nil {
		t.Fatalf("seal the root passphrase: %v", err)
	}

	share := map[string]any{
		"ShareID": shareID, "Key": shareKey, "Passphrase": sharePass,
		"PassphraseSignature": armoredSig,
		"PossibleKeyPackets": []map[string]string{
			{"KeyPacket": base64.StdEncoding.EncodeToString(split.GetBinaryKeyPacket())},
		},
		"RootLinkRecoveryPassphrase": sealedRoot,
	}
	body, err := json.Marshal(share)
	if err != nil {
		t.Fatalf("marshal the locked share: %v", err)
	}
	return &lockedTree{
		shareID: shareID, kind: kind, body: string(body),
		rootPassphrase: rootPassphrase, sessionKey: sessionKey,
	}
}

// restoreInto builds the tree the files come back into, and the service that
// serves the locked volume beside it.
func restoreInto(t *testing.T, u *keys.Unlocked, locked ...*lockedTree) (*Service, *Context, *pgp.KeyRing) {
	t.Helper()
	rings, _ := u.AddrRings(testAddrID)
	shareKey, err := pgp.GenerateKey("Share", "", "x25519", 0)
	if err != nil {
		t.Fatalf("generate the share key: %v", err)
	}
	shareKR, err := pgp.NewKeyRing(shareKey)
	if err != nil {
		t.Fatalf("share key ring: %v", err)
	}
	rootKey, rootPass, rootPassSig, rootPriv, err := genNodeKeys(shareKR, rings.Write)
	if err != nil {
		t.Fatalf("generate the root key: %v", err)
	}
	rootKR, err := pgp.NewKeyRing(rootPriv)
	if err != nil {
		t.Fatalf("root key ring: %v", err)
	}
	_, hashKey, err := genNodeHashKey(rootKR, rootKR)
	if err != nil {
		t.Fatalf("make the root's hash key: %v", err)
	}

	shares := make([]map[string]any, 0, len(locked))
	routes := map[string]string{}
	for _, l := range locked {
		shares = append(shares, map[string]any{
			"ShareID": l.shareID, "VolumeID": "old", "Type": l.kind, "Locked": true,
		})
		routes["GET /drive/shares/"+l.shareID] = l.body
	}
	list, err := json.Marshal(map[string]any{"Shares": shares})
	if err != nil {
		t.Fatalf("marshal the share listing: %v", err)
	}
	routes["GET /drive/shares"] = string(list)

	dc := &Context{
		ShareID: "active-share", ShareKR: shareKR,
		Addr: rings, AddrID: testAddrID, AddrEmail: "me@proton.me",
		addrKeys:   []keys.Key{{ID: "address-key", Primary: 1, Active: 1}},
		VolumeID:   "active",
		RootLinkID: "root",
		rootLink: &Link{
			LinkID: "root", NodeKey: rootKey,
			NodePassphrase: rootPass, NodePassphraseSignature: rootPassSig,
			FolderProperties: &FolderProperties{NodeHashKey: hashKey},
		},
	}
	return New(&stubDoer{routes: routes}, testKeys(u)), dc, rootKR
}

// What a restore hands Proton is what the new tree can open again: the old
// root's passphrase re-sealed to the root of the tree it is coming into, signed
// as that tree's address, and named by a hash the folder can be found under.
func TestRestoreHandsTheFilesBackSealedToTheNewTree(t *testing.T) {
	u := signedInAs(t, "me@proton.me")
	locked := lockLikeAReset(t, u, "old-share", shareTypeMain)
	s, dc, rootKR := restoreInto(t, u, locked)
	doer := s.C.(*stubDoer)

	restored, err := s.Restore(context.Background(), dc, "old")
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if restored.Files != 1 || restored.Computers != 0 || restored.Photos != 0 {
		t.Errorf("restored %+v, want one file share", restored)
	}
	if !strings.HasPrefix(restored.Folder, "Restored files ") {
		t.Errorf("folder = %q, want one named for the moment", restored.Folder)
	}

	var put proton.Request
	for _, r := range doer.reqs {
		if r.Method == "PUT" && r.Path == "/drive/volumes/old/restore" {
			put = r
		}
	}
	if put.Path == "" {
		t.Fatalf("requests were %v; want the restore", doer.reqs)
	}
	body, _ := put.Body.(map[string]any)
	if body["SignatureAddress"] != "me@proton.me" || body["AddressKeyID"] != "address-key" {
		t.Errorf("the restore is signed as %v with key %v", body["SignatureAddress"], body["AddressKeyID"])
	}
	mains, _ := body["MainShares"].([]map[string]any)
	if len(mains) != 1 {
		t.Fatalf("MainShares = %v, want the one locked share", body["MainShares"])
	}
	main := mains[0]
	if main["LockedShareID"] != "old-share" {
		t.Errorf("LockedShareID = %v, want the locked share", main["LockedShareID"])
	}

	passphrase, _ := main["NodePassphrase"].(string)
	msg, err := pgp.NewPGPMessageFromArmored(passphrase)
	if err != nil {
		t.Fatalf("the node passphrase is not readable armour: %v", err)
	}
	plain, err := rootKR.Decrypt(msg, nil, pgp.GetUnixTime())
	if err != nil {
		t.Fatalf("the node passphrase does not open with the new tree's root key: %v", err)
	}
	if plain.GetString() != locked.rootPassphrase {
		t.Error("the node passphrase is not the locked root's own")
	}

	signature, _ := main["NodePassphraseSignature"].(string)
	sig, err := pgp.NewPGPSignatureFromArmored(signature)
	if err != nil {
		t.Fatalf("the signature is not readable armour: %v", err)
	}
	if err := dc.Addr.Write.VerifyDetached(plain, sig, pgp.GetUnixTime()); err != nil {
		t.Errorf("the passphrase is not signed by the address the files come back to: %v", err)
	}
	if _, ok := main["Hash"].(string); !ok || main["Hash"] == "" {
		t.Error("the restored folder carries no lookup hash")
	}
	if _, ok := main["Name"].(string); !ok || main["Name"] == "" {
		t.Error("the restored folder carries no name")
	}
}

// A computer and the photo library are not moved: their share keys are handed
// back sealed to the address instead, each under the heading Proton expects.
func TestRestoreResealsComputersAndPhotosWhereTheyAre(t *testing.T) {
	u := signedInAs(t, "me@proton.me")
	device := lockLikeAReset(t, u, "old-device", shareTypeDevice)
	photos := lockLikeAReset(t, u, "old-photos", shareTypePhotos)
	s, dc, _ := restoreInto(t, u, device, photos)

	restored, err := s.Restore(context.Background(), dc, "old")
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if restored.Computers != 1 || restored.Photos != 1 || restored.Files != 0 {
		t.Fatalf("restored %+v, want one computer and one photo share", restored)
	}
	if restored.Folder != "" {
		t.Errorf("folder = %q, want none: nothing came back as files", restored.Folder)
	}

	body, _ := s.C.(*stubDoer).reqs[len(s.C.(*stubDoer).reqs)-1].Body.(map[string]any)
	for _, heading := range []string{"Devices", "PhotoShares"} {
		entries, _ := body[heading].([]map[string]any)
		if len(entries) != 1 {
			t.Fatalf("%s = %v, want one share", heading, body[heading])
		}
		packet, _ := entries[0]["ShareKeyPacket"].(string)
		raw, err := base64.StdEncoding.DecodeString(packet)
		if err != nil {
			t.Fatalf("%s: the key packet is not base64: %v", heading, err)
		}
		sk, err := dc.Addr.Write.DecryptSessionKey(raw)
		if err != nil {
			t.Fatalf("%s: the key packet does not open with the address's key: %v", heading, err)
		}
		want := device.sessionKey
		if heading == "PhotoShares" {
			want = photos.sessionKey
		}
		if string(sk.Key) != string(want.Key) {
			t.Errorf("%s: the key packet holds another share's session key", heading)
		}
	}
	if mains, _ := body["MainShares"].([]map[string]any); len(mains) != 0 {
		t.Errorf("MainShares = %v, want none", mains)
	}
}

// A volume whose keys are still locked is refused with the step that comes
// first, rather than with a decryption failure nobody can act on.
func TestRestoreRefusesWhileTheKeysAreLocked(t *testing.T) {
	owner := signedInAs(t, "me@proton.me")
	locked := lockLikeAReset(t, owner, "old-share", shareTypeMain)

	since := signedInAs(t, "me@proton.me")
	since.UserKeys = []keys.Key{{ID: "current", Active: 1}, {ID: "from-before-the-reset"}}
	s, dc, _ := restoreInto(t, since, locked)

	_, err := s.Restore(context.Background(), dc, "old")
	if err == nil {
		t.Fatal("a volume nothing opens was restored")
	}
	if !strings.Contains(err.Error(), "not active") {
		t.Errorf("err = %v, want it to say the keys are not active", err)
	}

	// With nothing locked the same failure is nobody's to act on, so it stays as
	// it is rather than sending the reader to reactivate keys that are fine.
	stranger := signedInAs(t, "me@proton.me")
	other, dcOther, _ := restoreInto(t, stranger, locked)
	_, err = other.Restore(context.Background(), dcOther, "old")
	if err == nil || strings.Contains(err.Error(), "not active") {
		t.Errorf("err = %v, want the failure left as it is", err)
	}
}

// Two file shares on one volume come back as two folders, numbered, so neither
// name takes the other's place.
func TestRestoreNamesASecondFolderApart(t *testing.T) {
	at := time.Date(2026, 4, 27, 14, 31, 0, 0, time.UTC)
	first, second := restoredFolderName(at, 0), restoredFolderName(at, 1)
	if first == second {
		t.Fatalf("both folders are named %q", first)
	}
	if !strings.HasPrefix(second, first) || !strings.HasSuffix(second, "(2)") {
		t.Errorf("the second folder is named %q, want %q numbered", second, first)
	}
	for _, forbidden := range []string{"/", "\\"} {
		if strings.Contains(first, forbidden) {
			t.Errorf("the folder name %q holds %q, which Drive refuses in a name", first, forbidden)
		}
	}
}

// Deleting a locked volume is one request and no local reasoning: Proton owns
// what happens to the files afterwards.
func TestDeleteLockedAsksProtonToDeleteIt(t *testing.T) {
	doer := &stubDoer{}
	if err := New(doer, testKeys(nil)).DeleteLocked(context.Background(), "old"); err != nil {
		t.Fatalf("DeleteLocked: %v", err)
	}
	if !doer.sent("PUT", "/drive/volumes/old/delete_locked") {
		t.Errorf("requests were %v, want the deletion", doer.reqs)
	}
}
