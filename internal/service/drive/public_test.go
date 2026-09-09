package drive

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
	// nobody is behind the writing.
	rootKR *pgp.KeyRing
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
	_, rootHashKey, err := genNodeHashKey(rootKR, rootKR)
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
			"NodeHashKey":    rootHashKey,
			"SignatureEmail": testLinkSigner, "ContentKeyPacket": "", "Size": 1234,
		}}),
		rootKR: rootKR,
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

// Everything read inside a link goes to the endpoints Proton serves a token
// under, because nobody outside the share has the share ID the others use.
func TestALinkIsReadThroughItsToken(t *testing.T) {
	tree := newPublicTree(t, testURLPassword, "Project", protonFolder)
	s, doer := publicService(t, tree, proton.PublicLinkGeneratedPassword, nil, map[string]string{
		"GET /drive/urls/" + testToken + "/folders/" + testRootID + "/children": `{"Links":[]}`,
	})

	dc, err := s.OpenLink(context.Background(), LinkURL(testToken, testURLPassword), "")
	if err != nil {
		t.Fatalf("OpenLink: %v", err)
	}
	if _, err := s.List(context.Background(), dc, "/"); err != nil {
		t.Fatalf("List: %v", err)
	}
	var paths []string
	for _, req := range doer.reqs {
		paths = append(paths, req.Method+" "+req.Path)
	}
	for _, want := range []string{
		"GET /drive/urls/" + testToken + "/info",
		"POST /drive/urls/" + testToken + "/auth",
		"GET /drive/urls/" + testToken,
		"GET /drive/urls/" + testToken + "/folders/" + testRootID + "/children",
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
		"GET /drive/urls/" + testToken + "/folders/" + testRootID + "/children":            `{"Links":[]}`,
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
		AddrKRs:   map[string]*pgp.KeyRing{testAddrID: addrKR},
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
