package drive

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"

	srp "github.com/ProtonMail/go-srp"
	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
	"github.com/roman-16/proton-cli/internal/proton"
)

// A link, the password its URL carries, and the salt Proton stores beside the
// share. The salt is 16 bytes because that is what bcrypt takes.
const (
	testToken       = "7X2K9M3N1P"
	testURLPassword = "kQ81mDx4T9wL"
	testLinkSalt    = "sixteen-byte-slt"
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
	rootKey, rootPass, rootPassSig, _, err := genNodeKeys(shareKR, shareKR)
	if err != nil {
		t.Fatalf("generate a root key: %v", err)
	}
	encName, err := encryptName(rootName, shareKR, shareKR)
	if err != nil {
		t.Fatalf("encrypt the root name: %v", err)
	}
	return &publicTree{
		share: &proton.PublicLinkShare{
			ShareKey: armoredKey, SharePassphrase: armoredPassphrase,
			SharePasswordSalt: base64.StdEncoding.EncodeToString([]byte(testLinkSalt)),
			VolumeID:          testVolumeID, LinkID: testRootID,
		},
		root: object(t, map[string]any{"Token": map[string]any{
			"Token": testToken, "LinkID": testRootID, "LinkType": rootType, "Name": encName,
			"NodeKey": rootKey, "NodePassphrase": rootPass, "NodePassphraseSignature": rootPassSig,
			"ContentKeyPacket": "", "Size": 1234,
		}}),
	}
}

func publicService(t *testing.T, tree *publicTree, flags int, extra map[string]string) (*Service, *stubDoer) {
	t.Helper()
	routes := map[string]string{"GET /drive/urls/" + testToken: tree.root}
	for path, body := range extra {
		routes[path] = body
	}
	doer := &stubDoer{
		routes:    routes,
		linkInfo:  &proton.PublicLinkInfo{Flags: flags, VendorType: proton.PublicLinkDrive},
		linkShare: tree.share,
	}
	return New(doer, testKeys(nil)), doer
}

// Opening a link proves the password its URL carried and unwraps the tree behind
// it, which is `share link`'s crypto read the other way round.
func TestOpeningALinkUnlocksTheTreeBehindIt(t *testing.T) {
	tree := newPublicTree(t, testURLPassword, "Q3-report.pdf", 2)
	s, doer := publicService(t, tree, proton.PublicLinkGeneratedPassword, nil)

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
	s, doer := publicService(t, tree, proton.PublicLinkCustomPassword|proton.PublicLinkGeneratedPassword, nil)

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
	s, doer := publicService(t, tree, proton.PublicLinkCustomPassword|proton.PublicLinkGeneratedPassword, nil)

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
	s, doer := publicService(t, tree, proton.PublicLinkGeneratedPassword, nil)
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
	s, doer := publicService(t, tree, proton.PublicLinkGeneratedPassword, map[string]string{
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
