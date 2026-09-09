package drive

import (
	"context"
	"encoding/json"
	"testing"

	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
	"github.com/roman-16/proton-cli/internal/account/keys"
)

// What Proton would answer about one share, and the identifiers a test names it
// by.
const (
	testAddrID   = "address-1"
	testAddrMail = "owner@proton.me"
	testShareID  = "share-1"
	testRootID   = "root-1"
	testVolumeID = "volume-1"
	testRootHash = "9c1f7e"
)

// tree is a share as Proton hands it over, and the keys that open it: an address
// key, the share key sealed to it, and a root sealed to the share key.
//
// It is built rather than canned because the crypto is the part worth testing:
// a name that decrypts here decrypts the way a real one does.
type tree struct {
	addrKR  *pgp.KeyRing
	shareKR *pgp.KeyRing
	share   string
	link    string
}

// withMembership is what Proton answers about a share somebody shared with you:
// your own standing in it, beside the keys that open it.
func (tr *tree) withMembership(t *testing.T, permissions int) *tree {
	t.Helper()
	var sh map[string]any
	if err := json.Unmarshal([]byte(tr.share), &sh); err != nil {
		t.Fatalf("read the canned share: %v", err)
	}
	sh["Memberships"] = []any{map[string]any{"Permissions": permissions}}
	tr.share = object(t, sh)
	return tr
}

func newTree(t *testing.T, shareType, rootType int, rootName string) *tree {
	t.Helper()
	addrKey, err := pgp.GenerateKey("Owner", testAddrMail, "x25519", 0)
	if err != nil {
		t.Fatalf("generate an address key: %v", err)
	}
	addrKR, err := pgp.NewKeyRing(addrKey)
	if err != nil {
		t.Fatalf("address key ring: %v", err)
	}
	shareKey, sharePass, sharePassSig, sharePriv, err := genNodeKeys(addrKR, addrKR)
	if err != nil {
		t.Fatalf("generate a share key: %v", err)
	}
	shareKR, err := pgp.NewKeyRing(sharePriv)
	if err != nil {
		t.Fatalf("share key ring: %v", err)
	}
	rootKey, rootPass, rootPassSig, _, err := genNodeKeys(shareKR, addrKR)
	if err != nil {
		t.Fatalf("generate a root key: %v", err)
	}
	encName, err := encryptName(rootName, shareKR, addrKR)
	if err != nil {
		t.Fatalf("encrypt the root name: %v", err)
	}
	return &tree{
		addrKR: addrKR, shareKR: shareKR,
		share: object(t, map[string]any{
			"AddressID": testAddrID, "Type": shareType, "Key": shareKey,
			"Passphrase": sharePass, "PassphraseSignature": sharePassSig,
		}),
		link: object(t, map[string]any{"Link": map[string]any{
			"LinkID": testRootID, "Type": rootType, "Name": encName, "Hash": testRootHash,
			"NodeKey": rootKey, "NodePassphrase": rootPass, "NodePassphraseSignature": rootPassSig,
		}}),
	}
}

func (tr *tree) keys() keys.Get {
	return testKeys(&keys.Unlocked{
		AddrKRs:   map[string]*pgp.KeyRing{testAddrID: tr.addrKR},
		Addresses: []keys.Address{{ID: testAddrID, Email: testAddrMail}},
	})
}

// routes are the two answers opening a share needs, keyed as the stub reads them.
func (tr *tree) routes() map[string]string {
	return map[string]string{
		"GET /drive/shares/" + testShareID:                          tr.share,
		"GET /drive/shares/" + testShareID + "/links/" + testRootID: tr.link,
	}
}

func (tr *tree) service(extra map[string]string) (*Service, *stubDoer) {
	routes := tr.routes()
	for path, body := range extra {
		routes[path] = body
	}
	doer := &stubDoer{routes: routes}
	return New(doer, tr.keys()), doer
}

func object(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("encode a canned response: %v", err)
	}
	return string(raw)
}

// The root of a tree is an item like any other: a computer's is a folder, and one
// somebody shared may be a single file - which is what lets `/` be downloaded
// rather than listed.
func TestTheRootResolvesAsTheItemItIs(t *testing.T) {
	for _, tc := range []struct {
		name     string
		linkType int
		isFolder bool
	}{
		{"a folder", protonFolder, true},
		{"a file", 2, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tr := newTree(t, shareTypeDevice, tc.linkType, "Work laptop")
			s, _ := tr.service(nil)
			dc, err := s.unlockShare(context.Background(), testShareID, testRootID, testVolumeID)
			if err != nil {
				t.Fatalf("unlockShare: %v", err)
			}
			res, err := s.ResolvePath(context.Background(), dc, "/")
			if err != nil {
				t.Fatalf("ResolvePath: %v", err)
			}
			if res.IsFolder != tc.isFolder {
				t.Errorf("IsFolder = %v, want %v", res.IsFolder, tc.isFolder)
			}
			if res.Name != "Work laptop" {
				t.Errorf("Name = %q, want the tree's own name", res.Name)
			}
			if !res.IsRoot() {
				t.Error("the root should report itself as one")
			}
		})
	}
}

// A file at the root of a shared item is downloadable, and a folder there is not:
// the refusal names the item rather than the slash that was typed.
func TestResolveFileRefusesAFolderByName(t *testing.T) {
	tr := newTree(t, shareTypeStandard, protonFolder, "Project")
	s, _ := tr.service(nil)
	dc, err := s.unlockShare(context.Background(), testShareID, testRootID, testVolumeID)
	if err != nil {
		t.Fatalf("unlockShare: %v", err)
	}
	_, err = s.ResolveFile(context.Background(), dc, "/")
	if err == nil {
		t.Fatal("a folder was accepted as a file")
	}
	if want := `Project is a folder, not a file.`; err.Error() != want {
		t.Errorf("error = %q, want %q", err.Error(), want)
	}
}

// What may be written into a tree is what the tree says as it opens, so an
// upload into one that will not take it is refused before anything is planned.
//
// A share of your own carries no membership: it is not somebody's grant, and
// there is nothing in it to check.
func TestWhatATreePermitsIsKnownWhenItOpens(t *testing.T) {
	for _, tc := range []struct {
		name    string
		share   func(*tree) *tree
		canEdit bool
		role    string
	}{
		{"your own", func(tr *tree) *tree { return tr }, true, ""},
		{"shared with you for viewing", func(tr *tree) *tree { return tr.withMembership(t, permView) }, false, "viewer"},
		{"shared with you for editing", func(tr *tree) *tree { return tr.withMembership(t, permEdit) }, true, "editor"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := tc.share(newTree(t, shareTypeStandard, protonFolder, "Project")).service(nil)
			dc, err := s.unlockShare(context.Background(), testShareID, testRootID, testVolumeID)
			if err != nil {
				t.Fatalf("unlockShare: %v", err)
			}
			if dc.CanEdit() != tc.canEdit {
				t.Errorf("CanEdit = %v, want %v", dc.CanEdit(), tc.canEdit)
			}
			if got := grantedRole(dc.Permissions); got != tc.role {
				t.Errorf("role = %q, want %q", got, tc.role)
			}
		})
	}
}

// Proton's own clients label the main and photo volumes rather than showing what
// is stored on their roots, so neither does this. Every other tree is named by
// the item at its top.
func TestOnlyATreeWithANameOfItsOwnReportsOne(t *testing.T) {
	for _, tc := range []struct {
		name      string
		shareType int
		want      string
	}{
		{"your own files", shareTypeMain, ""},
		{"the photo library", shareTypePhotos, ""},
		{"a computer", shareTypeDevice, "Work laptop"},
		{"something shared with you", shareTypeStandard, "Work laptop"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tr := newTree(t, tc.shareType, protonFolder, "Work laptop")
			s, _ := tr.service(nil)
			dc, err := s.unlockShare(context.Background(), testShareID, testRootID, testVolumeID)
			if err != nil {
				t.Fatalf("unlockShare: %v", err)
			}
			if dc.RootName != tc.want {
				t.Errorf("RootName = %q, want %q", dc.RootName, tc.want)
			}
		})
	}
}
