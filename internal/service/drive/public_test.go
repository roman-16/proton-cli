package drive

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	srp "github.com/ProtonMail/go-srp"
	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
	"github.com/roman-16/proton-cli/internal/account/keys"
	pgphelper "github.com/roman-16/proton-cli/internal/crypto/pgp"
	"github.com/roman-16/proton-cli/internal/proton"
)

// A link, the password its URL carries, and the salt Proton stores beside the
// share. The salt is 16 bytes because that is what bcrypt takes.
const (
	testToken       = "7X2K9M3N1P"
	testURLPassword = "kQ81mDx4T9wL"
	testLinkSalt    = "sixteen-byte-slt"
	testLinkSigner  = "jane@proton.me"
)

func TestParseLink(t *testing.T) {
	for _, tt := range []struct {
		raw      string
		token    string
		password string
		refused  bool
	}{
		{raw: "https://drive.proton.me/urls/7X2K9M3N1P#kQ81mDx4T9wL", token: testToken, password: testURLPassword},
		{raw: "  https://drive.proton.me/urls/7X2K9M3N1P#kQ81mDx4T9wL  ", token: testToken, password: testURLPassword},
		{raw: "https://drive.proton.me/urls/7X2K9M3N1P", token: testToken},
		{raw: "https://drive.proton.me/en/urls/7X2K9M3N1P#kQ81mDx4T9wL", token: testToken, password: testURLPassword},
		{raw: "https://example.com/somewhere", refused: true},
		{raw: "https://drive.proton.me/urls/", refused: true},
		{raw: "7X2K9M3N1P", refused: true},
		{raw: "", refused: true},
	} {
		token, password, err := ParseLink(tt.raw)
		if tt.refused {
			if err == nil {
				t.Errorf("ParseLink(%q) read a link out of it: %q", tt.raw, token)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseLink(%q): %v", tt.raw, err)
			continue
		}
		if token != tt.token || password != tt.password {
			t.Errorf("ParseLink(%q) = %q, %q; want %q, %q", tt.raw, token, password, tt.token, tt.password)
		}
	}
}

// A link's URL is what somebody was sent, so it is written back the way they
// would recognise it.
func TestLinkURLCarriesThePasswordItWasMadeWith(t *testing.T) {
	if got := LinkURL(testToken, testURLPassword); got != "https://drive.proton.me/urls/"+testToken+"#"+testURLPassword {
		t.Errorf("LinkURL = %q", got)
	}
	if got := LinkURL(testToken, ""); strings.Contains(got, "#") {
		t.Errorf("a link with no password should carry no fragment: %q", got)
	}
}

// publicTree is a public link as Proton hands it over: a share key locked with a
// passphrase that only the link's password opens, and a root sealed to it.
//
// The crypto is built rather than canned, so a name that decrypts here decrypts
// the way a real one does.
type publicTree struct {
	share *proton.PublicLinkShare
	root  string
	// rootKR is the key ring of what the link points at, which is what the names
	// and passphrases of everything written directly into it are signed with when
	// nobody is behind the writing. rootHashKey is what those names are hashed
	// under.
	rootKR      *pgp.KeyRing
	rootHashKey []byte
	// children is what each folder of the tree holds and items is what each of
	// those is, which are the two answers a link's tree is read through.
	children map[string][]string
	items    map[string]map[string]any
	// keys are the folders inside the link, which hold what is sealed to them.
	keys map[string]folderKeys
}

type folderKeys struct {
	nodeKR  *pgp.KeyRing
	hashKey []byte
}

func newPublicTree(t *testing.T, password, rootName string, rootType int) *publicTree {
	t.Helper()
	hashed, err := srp.MailboxPassword([]byte(password), []byte(testLinkSalt))
	if err != nil {
		t.Fatalf("derive the link's key password: %v", err)
	}
	keyPassword := hashed[len(hashed)-31:]

	passphrase := "the-share-passphrase"
	shareKey, err := pgp.GenerateKey("Link", "", "x25519", 0)
	if err != nil {
		t.Fatalf("generate a share key: %v", err)
	}
	locked, err := shareKey.Lock([]byte(passphrase))
	if err != nil {
		t.Fatalf("lock the share key: %v", err)
	}
	armoredKey, err := locked.Armor()
	if err != nil {
		t.Fatalf("armor the share key: %v", err)
	}
	sealed, err := pgp.EncryptMessageWithPassword(pgp.NewPlainMessageFromString(passphrase), keyPassword)
	if err != nil {
		t.Fatalf("seal the passphrase to the link's password: %v", err)
	}
	armoredPassphrase, err := sealed.GetArmored()
	if err != nil {
		t.Fatalf("armor the passphrase: %v", err)
	}
	shareKR, err := pgp.NewKeyRing(shareKey)
	if err != nil {
		t.Fatalf("share key ring: %v", err)
	}
	rootKey, rootPass, rootPassSig, rootPriv, err := genNodeKeys(shareKR, shareKR)
	if err != nil {
		t.Fatalf("generate a root key: %v", err)
	}
	rootKR, err := pgp.NewKeyRing(rootPriv)
	if err != nil {
		t.Fatalf("root key ring: %v", err)
	}
	rootHashKey, armouredRootHashKey, err := genNodeHashKey(rootKR, rootKR)
	if err != nil {
		t.Fatalf("generate the root's hash key: %v", err)
	}
	encName, err := encryptName(rootName, shareKR, shareKR)
	if err != nil {
		t.Fatalf("encrypt the root name: %v", err)
	}
	return &publicTree{
		share: &proton.PublicLinkShare{
			ShareKey: armoredKey, SharePassphrase: armoredPassphrase,
			SharePasswordSalt: base64.StdEncoding.EncodeToString([]byte(testLinkSalt)),
			VolumeID:          testVolumeID, LinkID: testRootID, PublicPermissions: permEdit,
		},
		root: object(t, map[string]any{"Token": map[string]any{
			"Token": testToken, "LinkID": testRootID, "LinkType": rootType, "Name": encName,
			"NodeKey": rootKey, "NodePassphrase": rootPass, "NodePassphraseSignature": rootPassSig,
			"NodeHashKey":    armouredRootHashKey,
			"SignatureEmail": testLinkSigner, "ContentKeyPacket": "", "Size": 1234,
		}}),
		rootKR: rootKR, rootHashKey: rootHashKey,
		children: map[string][]string{},
		items:    map[string]map[string]any{},
		keys:     map[string]folderKeys{},
	}
}

// The identifiers what is behind a link is named by.
const (
	testFileID   = "file-1"
	testFolderID = "folder-1"
)

// uploadedJustNow is when something was put into a link, for a test about
// anything other than the hour Proton allows it to be taken back in.
func uploadedJustNow() int64 { return time.Now().Add(-time.Minute).Unix() }

// hold puts items into a folder of the link's tree, sealing their names and
// keys to it, and hands the first of them back so a test can put things inside
// it in turn.
func (p *publicTree) hold(t *testing.T, parent string, items ...map[string]any) map[string]any {
	t.Helper()
	parentKR, hashKey := p.rootKR, p.rootHashKey
	if parent != testRootID {
		parentKR, hashKey = p.keyOf(t, parent)
	}
	for _, item := range items {
		link := item["Link"].(map[string]any)
		link["ParentLinkID"] = parent
		p.seal(t, item, parentKR, hashKey)
		id := link["LinkID"].(string)
		p.children[parent] = append(p.children[parent], id)
		p.items[id] = item
	}
	return items[0]
}

// file is one file behind a link, in the shape the endpoints serving one answer
// with: the item, and the version it holds.
func uploadedFile(name, uploadedBy string, at int64) map[string]any {
	details := uploadedItem(name, uploadedBy, at, testFileID, 2)
	details["File"] = map[string]any{
		"ContentKeyPacket": "", "MediaType": "image/jpeg",
		"ActiveRevision": map[string]any{"RevisionID": "rev-1", "EncryptedSize": 4096},
	}
	return details
}

// folder is one folder behind a link. What it holds is sealed to it, so its own
// keys are made when it is put into the tree.
func uploadedFolder(name, uploadedBy string, at int64) map[string]any {
	details := uploadedItem(name, uploadedBy, at, testFolderID, protonFolder)
	details["Folder"] = map[string]any{}
	return details
}

func uploadedItem(name, uploadedBy string, at int64, linkID string, linkType int) map[string]any {
	return map[string]any{"Link": map[string]any{
		"LinkID": linkID, "Type": linkType, "CreateTime": at, "ModifyTime": at,
		"SignatureEmail": uploadedBy, "name": name,
	}}
}

// seal gives an item the keys and the name it would have inside the folder it
// was put in: a name sealed to that folder, hashed under its hash key, and a
// node key the folder opens.
func (p *publicTree) seal(t *testing.T, details map[string]any, parentKR *pgp.KeyRing, hashKey []byte) {
	t.Helper()
	link := details["Link"].(map[string]any)
	name := link["name"].(string)
	delete(link, "name")

	nodeKey, nodePass, nodePassSig, nodePriv, err := genNodeKeys(parentKR, parentKR)
	if err != nil {
		t.Fatalf("generate a node key: %v", err)
	}
	encName, err := encryptName(name, parentKR, parentKR)
	if err != nil {
		t.Fatalf("encrypt a name: %v", err)
	}
	hash, err := lookupHash(strings.ToLower(name), hashKey)
	if err != nil {
		t.Fatalf("hash a name: %v", err)
	}
	link["NodeKey"], link["NodePassphrase"], link["NodePassphraseSignature"] = nodeKey, nodePass, nodePassSig
	link["Name"], link["NameHash"] = encName, hash

	if _, isFolder := details["Folder"]; !isFolder {
		return
	}
	nodeKR, err := pgp.NewKeyRing(nodePriv)
	if err != nil {
		t.Fatalf("node key ring: %v", err)
	}
	ownHashKey, armoured, err := genNodeHashKey(nodeKR, nodeKR)
	if err != nil {
		t.Fatalf("generate a hash key: %v", err)
	}
	details["Folder"] = map[string]any{"NodeHashKey": armoured}
	p.keys[link["LinkID"].(string)] = folderKeys{nodeKR: nodeKR, hashKey: ownHashKey}
}

// keyOf is what a folder inside the link opens with, which is what the names of
// everything inside it are sealed to.
func (p *publicTree) keyOf(t *testing.T, linkID string) (*pgp.KeyRing, []byte) {
	t.Helper()
	keys, ok := p.keys[linkID]
	if !ok {
		t.Fatalf("%s is not a folder of this link", linkID)
	}
	return keys.nodeKR, keys.hashKey
}

// answers serves the two endpoints a link's tree is read through. Neither can be
// canned per path: what a folder holds, and what those items are, are questions
// about whatever the test put in the tree.
func (p *publicTree) answers(t *testing.T) func(proton.Request) []byte {
	t.Helper()
	folders := "/drive/unauth/v2/volumes/" + testVolumeID + "/folders/"
	return func(r proton.Request) []byte {
		switch {
		case r.Method == "GET" && strings.HasPrefix(r.Path, folders):
			parent := strings.TrimSuffix(strings.TrimPrefix(r.Path, folders), "/children")
			return []byte(object(t, map[string]any{"LinkIDs": p.children[parent], "More": false}))
		case r.Method == "POST" && r.Path == "/drive/unauth/v2/volumes/"+testVolumeID+"/links":
			asked, _ := r.Body.(map[string]any)["LinkIDs"].([]string)
			wanted := make([]map[string]any, 0, len(asked))
			for _, id := range asked {
				wanted = append(wanted, p.items[id])
			}
			return []byte(object(t, map[string]any{"Links": wanted}))
		}
		return nil
	}
}

// signedInAs is the key hierarchy of somebody holding one address, which is what
// decides whether an item behind a link is theirs to take back.
func signedInAs(t *testing.T, email string) *keys.Unlocked {
	t.Helper()
	addrKey, err := pgp.GenerateKey("Owner", email, "x25519", 0)
	if err != nil {
		t.Fatalf("generate an address key: %v", err)
	}
	addrKR, err := pgp.NewKeyRing(addrKey)
	if err != nil {
		t.Fatalf("address key ring: %v", err)
	}
	return &keys.Unlocked{
		AddrKRs:   map[string]keys.Rings{testAddrID: {Read: addrKR, Write: addrKR}},
		Addresses: []keys.Address{{ID: testAddrID, Email: email}},
	}
}

// publicService opens a link as whoever holds u, which is nobody when it is nil.
//
// Proton mints a session of its own only for a caller it cannot recognise, so a
// service with no keys is one nobody is behind - and one that asks for keys it
// was never given fails rather than quietly signing as somebody.
func publicService(t *testing.T, tree *publicTree, flags int, u *keys.Unlocked, extra map[string]string) (*Service, *stubDoer) {
	t.Helper()
	routes := map[string]string{"GET /drive/urls/" + testToken: tree.root}
	for path, body := range extra {
		routes[path] = body
	}
	tree.share.Anonymous = u == nil
	doer := &stubDoer{
		routes:    routes,
		answers:   tree.answers(t),
		linkInfo:  &proton.PublicLinkInfo{Flags: flags, VendorType: proton.PublicLinkDrive},
		linkShare: tree.share,
	}
	return New(doer, testKeys(u)), doer
}

// Opening a link proves the password its URL carried and unwraps the tree behind
// it, which is `share link`'s crypto read the other way round.
func TestOpeningALinkUnlocksTheTreeBehindIt(t *testing.T) {
	tree := newPublicTree(t, testURLPassword, "Q3-report.pdf", 2)
	s, doer := publicService(t, tree, proton.PublicLinkGeneratedPassword, nil, nil)

	dc, err := s.OpenLink(context.Background(), LinkURL(testToken, testURLPassword), "")
	if err != nil {
		t.Fatalf("OpenLink: %v", err)
	}
	if doer.proved != testURLPassword {
		t.Errorf("proved %q, want the password from the URL", doer.proved)
	}
	if !dc.Public() || dc.Token != testToken {
		t.Errorf("the tree is not the link: public=%v token=%q", dc.Public(), dc.Token)
	}
	if dc.RootName != "Q3-report.pdf" {
		t.Errorf("root name %q, want the decrypted one", dc.RootName)
	}
	if dc.URL != LinkURL(testToken, testURLPassword) {
		t.Errorf("the tree carries url %q", dc.URL)
	}
	if _, err := dc.RootKR(); err != nil {
		t.Errorf("the root did not open: %v", err)
	}
}

// A link whose owner set a password is proved with both halves, in the order the
// two were concatenated when it was made.
func TestALinkWithACustomPasswordProvesBothHalves(t *testing.T) {
	tree := newPublicTree(t, testURLPassword+"hunter2", "Q3-report.pdf", 2)
	s, doer := publicService(t, tree, proton.PublicLinkCustomPassword|proton.PublicLinkGeneratedPassword, nil, nil)

	if _, err := s.OpenLink(context.Background(), LinkURL(testToken, testURLPassword), "hunter2"); err != nil {
		t.Fatalf("OpenLink: %v", err)
	}
	if doer.proved != testURLPassword+"hunter2" {
		t.Errorf("proved %q, want both halves", doer.proved)
	}
}

// A password that was not given is asked for before anything is proved, and the
// refusal says where a password may come from.
func TestALinkWithACustomPasswordSaysWhenItIsMissing(t *testing.T) {
	tree := newPublicTree(t, testURLPassword+"hunter2", "Q3-report.pdf", 2)
	s, doer := publicService(t, tree, proton.PublicLinkCustomPassword|proton.PublicLinkGeneratedPassword, nil, nil)

	_, err := s.OpenLink(context.Background(), LinkURL(testToken, testURLPassword), "")
	if err == nil || !strings.Contains(err.Error(), "This link has a password") {
		t.Fatalf("OpenLink refused with %v", err)
	}
	if doer.proved != "" {
		t.Error("a password was proved even though none was given")
	}
}

// A link to another Proton product shares the URL shape and nothing else, so it
// is refused with somewhere to go rather than opened and found empty.
func TestALinkToAnotherProtonProductIsRefused(t *testing.T) {
	tree := newPublicTree(t, testURLPassword, "Notes", 2)
	s, doer := publicService(t, tree, proton.PublicLinkGeneratedPassword, nil, nil)
	doer.linkInfo = &proton.PublicLinkInfo{
		Flags: proton.PublicLinkGeneratedPassword, VendorType: proton.PublicLinkDoc,
	}

	_, err := s.OpenLink(context.Background(), LinkURL(testToken, testURLPassword), "")
	if err == nil || !strings.Contains(err.Error(), "Proton Docs document") {
		t.Fatalf("OpenLink refused with %v", err)
	}
}

// A link is opened by the token that names it and read through the endpoints
// that answer without an account, and never through a share ID nobody outside
// the share has.
func TestALinkIsOpenedByTokenAndReadWithoutAnAccount(t *testing.T) {
	tree := newPublicTree(t, testURLPassword, "Project", protonFolder)
	s, doer := publicService(t, tree, proton.PublicLinkGeneratedPassword, nil, nil)
	tree.hold(t, testRootID, uploadedFile("photo.jpg", testLinkSigner, uploadedJustNow()))

	dc, err := s.OpenLink(context.Background(), LinkURL(testToken, testURLPassword), "")
	if err != nil {
		t.Fatalf("OpenLink: %v", err)
	}
	children, err := s.List(context.Background(), dc, "/")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(children) != 1 || children[0].Name != "photo.jpg" || children[0].Type != TypeFile {
		t.Fatalf("the link's tree listed as %+v", children)
	}
	if children[0].Size != 4096 {
		t.Errorf("size = %d, want the size of the version the file holds", children[0].Size)
	}

	var paths []string
	for _, req := range doer.reqs {
		paths = append(paths, req.Method+" "+req.Path)
	}
	for _, want := range []string{
		"GET /drive/urls/" + testToken + "/info",
		"POST /drive/urls/" + testToken + "/auth",
		"GET /drive/urls/" + testToken,
		"GET /drive/unauth/v2/volumes/" + testVolumeID + "/folders/" + testRootID + "/children",
		"POST /drive/unauth/v2/volumes/" + testVolumeID + "/links",
	} {
		if !containsString(paths, want) {
			t.Errorf("%s was never sent; sent %v", want, paths)
		}
	}
	for _, req := range doer.reqs {
		if strings.Contains(req.Path, "/drive/shares/") {
			t.Errorf("a public link asked about a share it has no ID for: %s", req.Path)
		}
	}
}

// What a link's reader is told about an item includes who uploaded it, which is
// what decides whether they may take it back.
func TestALinksTreeNamesWhoUploadedWhat(t *testing.T) {
	tree := newPublicTree(t, testURLPassword, "Project", protonFolder)
	s, _ := publicService(t, tree, proton.PublicLinkGeneratedPassword, signedInAs(t, testLinkSigner), nil)
	tree.hold(t, testRootID, uploadedFile("photo.jpg", testLinkSigner, uploadedJustNow()))

	dc, err := s.OpenLink(context.Background(), LinkURL(testToken, testURLPassword), "")
	if err != nil {
		t.Fatalf("OpenLink: %v", err)
	}
	res, err := s.ResolvePath(context.Background(), dc, "/photo.jpg")
	if err != nil {
		t.Fatalf("ResolvePath: %v", err)
	}
	if res.Link.SignatureEmail != testLinkSigner {
		t.Errorf("the item names %q as its uploader, want %s", res.Link.SignatureEmail, testLinkSigner)
	}
	if res.Parent == nil || res.Parent.LinkID != testRootID {
		t.Error("the item does not carry the folder it was found in, which a rename hashes under")
	}
}

// An upload nobody finished is not an item: it holds no version to read, name or
// count, and Proton's own clients leave one out of a listing too.
func TestAnUnfinishedUploadIsNotListedInALink(t *testing.T) {
	tree := newPublicTree(t, testURLPassword, "Project", protonFolder)
	draft := uploadedFile("half-sent.jpg", testLinkSigner, uploadedJustNow())
	draft["File"].(map[string]any)["ActiveRevision"] = nil
	s, _ := publicService(t, tree, proton.PublicLinkGeneratedPassword, nil, nil)
	tree.hold(t, testRootID, draft)

	dc, err := s.OpenLink(context.Background(), LinkURL(testToken, testURLPassword), "")
	if err != nil {
		t.Fatalf("OpenLink: %v", err)
	}
	children, err := s.List(context.Background(), dc, "/")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(children) != 0 {
		t.Errorf("a draft was listed as an item: %+v", children)
	}
}

// A link takes back only what this account put there, and only while it is new.
// Both are judged from the listing, before anything is asked of Proton.
func TestALinkOnlyGivesBackYourOwnRecentUploads(t *testing.T) {
	anHourAndAHalfAgo := time.Now().Add(-90 * time.Minute).Unix()
	for _, tc := range []struct {
		name       string
		uploadedBy string
		at         int64
		want       string
	}{
		{name: "somebody else's", uploadedBy: "stranger@proton.me", at: uploadedJustNow(),
			want: "/photo.jpg is not yours to rename here."},
		{name: "nobody's", uploadedBy: "", at: uploadedJustNow(),
			want: "/photo.jpg is not yours to rename here."},
		{name: "yours, an hour and a half ago", uploadedBy: testAddrMail, at: anHourAndAHalfAgo,
			want: "/photo.jpg was uploaded more than an hour ago, so it is not yours to rename any more."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tree := newPublicTree(t, testURLPassword, "Project", protonFolder)
			s, doer := publicService(t, tree, proton.PublicLinkGeneratedPassword, signedInAs(t, testAddrMail), nil)
			tree.hold(t, testRootID, uploadedFile("photo.jpg", tc.uploadedBy, tc.at))

			dc, err := s.OpenLink(context.Background(), LinkURL(testToken, testURLPassword), "")
			if err != nil {
				t.Fatalf("OpenLink: %v", err)
			}
			err = s.Rename(context.Background(), dc, "/photo.jpg", "holiday.jpg")
			if err == nil || err.Error() != tc.want {
				t.Fatalf("Rename refused with %v, want %q", err, tc.want)
			}
			if doer.sent("PUT", "/drive/unauth/v2/volumes/"+testVolumeID+"/links/"+testFileID+"/rename") {
				t.Error("a rename Proton would refuse was sent anyway")
			}
		})
	}
}

// Renaming your own recent upload goes to the link's own endpoint, signed by the
// address that put it there and naming both hashes: the one the new name takes
// and the one it releases.
func TestRenamingYourOwnUploadInALinkNamesBothHashes(t *testing.T) {
	tree := newPublicTree(t, testURLPassword, "Project", protonFolder)
	s, doer := publicService(t, tree, proton.PublicLinkGeneratedPassword, signedInAs(t, testAddrMail), nil)
	tree.hold(t, testRootID, uploadedFile("photo.jpg", testAddrMail, uploadedJustNow()))

	dc, err := s.OpenLink(context.Background(), LinkURL(testToken, testURLPassword), "")
	if err != nil {
		t.Fatalf("OpenLink: %v", err)
	}
	if err := s.Rename(context.Background(), dc, "/photo.jpg", "holiday.jpg"); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	req := doer.last()
	if req.Method != "PUT" || req.Path != "/drive/unauth/v2/volumes/"+testVolumeID+"/links/"+testFileID+"/rename" {
		t.Fatalf("the rename went to %s %s", req.Method, req.Path)
	}
	body, ok := req.Body.(map[string]any)
	if !ok {
		t.Fatalf("the rename carried %T", req.Body)
	}
	if body["NameSignatureEmail"] != testAddrMail {
		t.Errorf("the new name is signed by %v, want %s", body["NameSignatureEmail"], testAddrMail)
	}
	if body["Hash"] == "" || body["OriginalHash"] == "" || body["Hash"] == body["OriginalHash"] {
		t.Errorf("the rename names hashes %v and %v", body["Hash"], body["OriginalHash"])
	}
	name, err := decryptName(body["Name"].(string), tree.rootKR)
	if err != nil || name != "holiday.jpg" {
		t.Errorf("the new name reads %q (%v), and is sealed to the folder it sits in", name, err)
	}
}

// Deleting in a link is deleting for good, and a folder means its contents: what
// is inside is given back in a round of its own, before the folder holding it.
func TestDeletingAFolderInALinkGivesBackItsContentsFirst(t *testing.T) {
	tree := newPublicTree(t, testURLPassword, "Project", protonFolder)
	s, doer := publicService(t, tree, proton.PublicLinkGeneratedPassword, signedInAs(t, testAddrMail), nil)
	tree.hold(t, testRootID, uploadedFolder("album", testAddrMail, uploadedJustNow()))
	tree.hold(t, testFolderID, uploadedFile("photo.jpg", testAddrMail, uploadedJustNow()))

	dc, err := s.OpenLink(context.Background(), LinkURL(testToken, testURLPassword), "")
	if err != nil {
		t.Fatalf("OpenLink: %v", err)
	}
	plan, err := s.PlanDelete(context.Background(), dc, []Child{{LinkID: testFolderID, Path: "/album", Type: TypeFolder}})
	if err != nil {
		t.Fatalf("PlanDelete: %v", err)
	}
	if len(plan.Refused) != 0 {
		t.Fatalf("your own recent upload was refused: %v", plan.Refused)
	}
	if got := plan.deep; len(got) != 2 || got[0][0] != testFolderID || got[1][0] != testFileID {
		t.Fatalf("the removal names %v, want the folder above what is inside it", got)
	}
	if _, err := s.Delete(context.Background(), dc, plan); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// One round per depth, from the bottom: a request that named a folder and
	// what is in it together would be refused for the folder that is not empty
	// yet, however the two were ordered inside it.
	var removals [][]string
	for _, req := range doer.reqs {
		if req.Path != "/drive/unauth/v2/volumes/"+testVolumeID+"/remove-mine" {
			continue
		}
		named, _ := req.Body.(map[string]any)["LinkIDs"].([]string)
		removals = append(removals, named)
	}
	if len(removals) != 2 {
		t.Fatalf("the deletion went out in %d requests, want one per depth: %v", len(removals), removals)
	}
	if len(removals[0]) != 1 || removals[0][0] != testFileID {
		t.Errorf("the first request named %v, want what is inside the folder", removals[0])
	}
	if len(removals[1]) != 1 || removals[1][0] != testFolderID {
		t.Errorf("the second request named %v, want the folder", removals[1])
	}
	if doer.sent("POST", "/drive/v2/volumes/"+testVolumeID+"/trash_multiple") {
		t.Error("a link has no trash, but something was trashed on the way")
	}
}

// A folder holding something this account did not upload is refused whole: the
// refusal is about the thing that was chosen, not about a file nobody named, and
// it comes before the question rather than after the answer.
func TestDeletingAFolderHoldingSomebodyElsesUploadIsRefused(t *testing.T) {
	tree := newPublicTree(t, testURLPassword, "Project", protonFolder)
	s, doer := publicService(t, tree, proton.PublicLinkGeneratedPassword, signedInAs(t, testAddrMail), nil)
	tree.hold(t, testRootID, uploadedFolder("album", testAddrMail, uploadedJustNow()))
	tree.hold(t, testFolderID, uploadedFile("photo.jpg", "stranger@proton.me", uploadedJustNow()))

	dc, err := s.OpenLink(context.Background(), LinkURL(testToken, testURLPassword), "")
	if err != nil {
		t.Fatalf("OpenLink: %v", err)
	}
	plan, err := s.PlanDelete(context.Background(), dc, []Child{{LinkID: testFolderID, Path: "/album", Type: TypeFolder}})
	if err != nil {
		t.Fatalf("PlanDelete: %v", err)
	}
	if len(plan.deep) != 0 {
		t.Errorf("a refused folder still named %v to remove", plan.deep)
	}
	if len(plan.Refused) != 1 {
		t.Fatalf("the refusals are %v, want one about the folder that was chosen", plan.Refused)
	}
	said := plan.Refused[0].String()
	want := "/album holds something that is not yours to delete. " +
		"In a link you can delete only what you uploaded yourself, within an hour of uploading it."
	if said != want {
		t.Errorf("the refusal reads %q, want %q", said, want)
	}
	if doer.sent("POST", "/drive/unauth/v2/volumes/"+testVolumeID+"/remove-mine") {
		t.Error("a deletion was sent for something judged unremovable")
	}
}

func containsString(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

// linkUpload is a service ready to take one file into a link, and the storage
// the block lands in.
//
// The whole of an upload is canned: a link that answers where to put a file,
// where to put its blocks and what to verify them against is what makes the
// requests, the signatures and the bodies a test can look at.
func linkUpload(t *testing.T, tree *publicTree, u *keys.Unlocked) (*Service, *stubDoer) {
	t.Helper()
	storage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(storage.Close)
	verification := object(t, map[string]any{
		"VerificationCode": base64.StdEncoding.EncodeToString([]byte("verify-these-bytes")),
	})
	return publicService(t, tree, proton.PublicLinkGeneratedPassword, u, map[string]string{
		"POST /drive/urls/" + testToken + "/files":                                         `{"File":{"ID":"file-1","RevisionID":"rev-1"}}`,
		"GET /drive/urls/" + testToken + "/links/file-1/revisions/rev-1/verification":      verification,
		"POST /drive/urls/" + testToken + "/blocks":                                        object(t, map[string]any{"UploadLinks": []any{map[string]any{"Token": "block-token", "BareURL": storage.URL}}}),
		"PUT /drive/urls/" + testToken + "/files/file-1/revisions/rev-1":                   `{}`,
		"POST /drive/urls/" + testToken + "/files/" + testRootID + "/checkAvailableHashes": `{"AvailableHashes":[]}`,
	})
}

// uploadInto puts one file into an open link and hands back the bodies the two
// requests that describe it carried.
func uploadInto(t *testing.T, s *Service, doer *stubDoer, dc *Context, name string) (draft, commit map[string]any) {
	t.Helper()
	plan, err := s.PlanUpload(context.Background(), dc, "/", name, ConflictRefuse)
	if err != nil {
		t.Fatalf("PlanUpload: %v", err)
	}
	if err := s.Upload(context.Background(), dc, plan, strings.NewReader("the bytes"), UploadOptions{}); err != nil {
		t.Fatalf("Upload: %v", err)
	}
	for _, req := range doer.reqs {
		body, ok := req.Body.(map[string]any)
		if !ok {
			continue
		}
		switch {
		case req.Path == "/drive/urls/"+testToken+"/files":
			draft = body
		case req.Method == "PUT":
			commit = body
		}
	}
	if draft == nil || commit == nil {
		t.Fatalf("the upload sent no file draft or no commit: %v", doer.reqs)
	}
	return draft, commit
}

// A link that allows editing takes a file, and every request that puts it there
// goes to the endpoints Proton serves a token under.
func TestALinkIsWrittenThroughItsToken(t *testing.T) {
	tree := newPublicTree(t, testURLPassword, "Project", protonFolder)
	s, doer := linkUpload(t, tree, nil)

	dc, err := s.OpenLink(context.Background(), LinkURL(testToken, testURLPassword), "")
	if err != nil {
		t.Fatalf("OpenLink: %v", err)
	}
	if !dc.CanEdit() {
		t.Fatal("a link Proton says allows editing should report as much")
	}
	_, commit := uploadInto(t, s, doer, dc, "photo.jpg")

	var paths []string
	for _, req := range doer.reqs {
		paths = append(paths, req.Method+" "+req.Path)
	}
	for _, want := range []string{
		"POST /drive/urls/" + testToken + "/files",
		"GET /drive/urls/" + testToken + "/links/file-1/revisions/rev-1/verification",
		"POST /drive/urls/" + testToken + "/blocks",
		"PUT /drive/urls/" + testToken + "/files/file-1/revisions/rev-1",
	} {
		if !containsString(paths, want) {
			t.Errorf("%s was never sent; sent %v", want, paths)
		}
	}
	for _, req := range doer.reqs {
		if strings.Contains(req.Path, "/drive/shares/") || req.Path == "/drive/blocks" {
			t.Errorf("an upload into a public link named a share it has no ID for: %s", req.Path)
		}
	}
	// The blocks are Proton's to have collected as they arrived, and the state a
	// committed revision is in is its own.
	for _, unwanted := range []string{"BlockList", "State"} {
		if _, ok := commit[unwanted]; ok {
			t.Errorf("the commit carries %s, which no client sends", unwanted)
		}
	}
	// A link's endpoints refuse a revision that does not describe itself.
	for _, wanted := range []string{"ManifestSignature", "XAttr"} {
		if armored, _ := commit[wanted].(string); !strings.HasPrefix(armored, "-----BEGIN PGP") {
			t.Errorf("the commit carries no %s", wanted)
		}
	}
}

// A file written into a link with nobody behind it names nobody, and is signed
// with the key it hangs from - so what it says about itself is checkable by
// everyone who can read the link at all.
func TestWritingIntoALinkAsNobodySignsWithTheParentKey(t *testing.T) {
	tree := newPublicTree(t, testURLPassword, "Project", protonFolder)
	s, doer := linkUpload(t, tree, nil)

	dc, err := s.OpenLink(context.Background(), LinkURL(testToken, testURLPassword), "")
	if err != nil {
		t.Fatalf("OpenLink: %v", err)
	}
	if !dc.Anonymous {
		t.Fatal("a link that answered with a session of its own has nobody behind it")
	}
	draft, commit := uploadInto(t, s, doer, dc, "photo.jpg")

	for _, body := range []map[string]any{draft, commit} {
		for _, named := range []string{"SignatureEmail", "SignatureAddress"} {
			if _, ok := body[named]; ok {
				t.Errorf("a write nobody is behind named somebody in %s", named)
			}
		}
	}
	passphrase, ok := draft["NodePassphrase"].(string)
	if !ok {
		t.Fatalf("the draft carries no node passphrase: %v", draft)
	}
	signature, ok := draft["NodePassphraseSignature"].(string)
	if !ok {
		t.Fatalf("the draft carries no passphrase signature: %v", draft)
	}
	enc, err := pgp.NewPGPMessageFromArmored(passphrase)
	if err != nil {
		t.Fatalf("read the passphrase: %v", err)
	}
	dec, err := tree.rootKR.Decrypt(enc, nil, pgp.GetUnixTime())
	if err != nil {
		t.Fatalf("the passphrase is not sealed to what the link points at: %v", err)
	}
	norm := pgp.NewPlainMessageFromString(string(dec.GetBinary()))
	if verdict := pgphelper.VerifyDetachedStatus(tree.rootKR, norm, signature); verdict != pgphelper.Verified {
		t.Errorf("the passphrase signature is %s against the key it hangs from", verdict)
	}
}

// Signed in, a file written into a link names the address that wrote it, which
// is what shows the link's owner who uploaded into their folder.
func TestWritingIntoALinkSignedInNamesYourAddress(t *testing.T) {
	addrKey, err := pgp.GenerateKey("Owner", testAddrMail, "x25519", 0)
	if err != nil {
		t.Fatalf("generate an address key: %v", err)
	}
	addrKR, err := pgp.NewKeyRing(addrKey)
	if err != nil {
		t.Fatalf("address key ring: %v", err)
	}
	u := &keys.Unlocked{
		AddrKRs:   map[string]keys.Rings{testAddrID: {Read: addrKR, Write: addrKR}},
		Addresses: []keys.Address{{ID: testAddrID, Email: testAddrMail}},
	}
	tree := newPublicTree(t, testURLPassword, "Project", protonFolder)
	s, doer := linkUpload(t, tree, u)

	dc, err := s.OpenLink(context.Background(), LinkURL(testToken, testURLPassword), "")
	if err != nil {
		t.Fatalf("OpenLink: %v", err)
	}
	if dc.Anonymous {
		t.Fatal("a link opened by somebody signed in has them behind it")
	}
	draft, commit := uploadInto(t, s, doer, dc, "photo.jpg")

	for what, body := range map[string]map[string]any{"draft": draft, "commit": commit} {
		if body["SignatureEmail"] != testAddrMail {
			t.Errorf("the %s names %v as the author, want %s", what, body["SignatureEmail"], testAddrMail)
		}
		if _, ok := body["SignatureAddress"]; ok {
			t.Errorf("the %s names the author under SignatureAddress, which a link's endpoints do not read", what)
		}
	}
}

// Whether a name is free is a question, and asking it is how a preview promises
// the name a file would really land under - so a dry run may send it.
func TestTheNameCheckIsAReadDespiteBeingAPost(t *testing.T) {
	for _, dc := range []*Context{{Token: testToken}, {ShareID: testShareID}} {
		req := hashesRequest(dc, testRootID, []string{"a-hash"})
		if req.Method != "POST" {
			t.Errorf("the name check is a %s", req.Method)
		}
		if !req.Reads {
			t.Errorf("%s %s does not say it only reads, so --dry-run would refuse it", req.Method, req.Path)
		}
	}
}

// A saved password is stored as one string and read back as two, so a link saved
// with a password of its own opens again without anything being typed.
func TestSplitLinkPassword(t *testing.T) {
	url, custom := splitLinkPassword(testURLPassword + "hunter2")
	if url != testURLPassword || custom != "hunter2" {
		t.Errorf("split = %q, %q", url, custom)
	}
	if url, custom := splitLinkPassword(testURLPassword); url != testURLPassword || custom != "" {
		t.Errorf("a link with no password of its own split into %q, %q", url, custom)
	}
}
