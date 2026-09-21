package drive

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
	"github.com/roman-16/proton-cli/internal/progress"
	"github.com/roman-16/proton-cli/internal/proton"
	"github.com/roman-16/proton-cli/internal/search"
)

// A tree to index, as Proton would hand it over.
//
// Indexing Drive is the walk the listings already do, written down. So what is
// worth checking is that the two answer with the same tree, and that what
// happens to it afterwards reaches the index through the volume's feed rather
// than through another walk.

// indexedTree is a Drive service whose tree is canned, whose index is a directory of
// its own, and whose keys are the ones the tree is sealed with.
type indexedTree struct {
	s    *Service
	tr   *tree
	doer *stubDoer
	dc   *Context
	root *cannedFolder
	// storage serves the blocks of the files in the tree, the way Proton's
	// storage hosts do: a URL of its own, outside the API.
	storage *httptest.Server
	blocks  map[string][]byte
}

// notesText is what the one text file in the canned tree says, for the tests
// about what a keyword reads.
const notesText = "Where to leave the car: the vienna parking permit is in the glovebox."

// newIndexed builds a small tree: two files at the top, a folder, and a file
// inside it - the shape a path has to survive.
func newIndexedTree(t *testing.T) *indexedTree {
	t.Helper()
	tr := newTree(t, shareTypeMain, protonFolder, "root")
	s, doer := tr.service(map[string]string{
		"GET /drive/volumes": volumeList(`{"VolumeID":"` + testVolumeID + `","State":1,"Type":1,` +
			`"Share":{"ShareID":"` + testShareID + `","LinkID":"` + testRootID + `"}}`),
		// A folder with nothing in it still answers.
		"GET /drive/shares/" + testShareID + "/folders/" + testRootID + "/children": `{"Links":[]}`,
		"GET /drive/volumes/" + testVolumeID + "/events/latest":                     `{"EventID":"cursor-0"}`,
		"GET /drive/volumes/" + testVolumeID + "/events/cursor-0":                   `{"EventID":"cursor-1","More":0,"Events":[]}`,
	})
	s.SetIndex(search.New(t.TempDir(), func() string { return "user-1" },
		func(context.Context) (search.Keys, error) {
			return search.Keys{Seal: tr.addrKR, Open: tr.addrKR}, nil
		}))

	// Proton pages a revision's blocks and answers past the end with none of
	// them, which is what tells a download it has them all.
	doer.answers = func(r proton.Request) []byte {
		if !strings.Contains(r.Path, "/revisions/") || r.Query.Get("FromBlockIndex") == "1" {
			return nil
		}
		return []byte(`{"Revision":{"Blocks":[]}}`)
	}

	d := &indexedTree{s: s, tr: tr, doer: doer, blocks: map[string][]byte{}}
	d.storage = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		block, ok := d.blocks[strings.TrimPrefix(r.URL.Path, "/")]
		if !ok {
			http.Error(w, "no such block", http.StatusNotFound)
			return
		}
		_, _ = w.Write(block)
	}))
	t.Cleanup(d.storage.Close)
	dc, err := s.Resolve(t.Context())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	d.dc = dc

	d.root = d.folder(t, testRootID, d.rootKey(t))
	reports := d.add(t, d.root, "folder-1", "Reports", protonFolder, 0)
	d.add(t, d.root, "invoice.pdf", "invoice.pdf", protonFile, 312)
	d.addFile(t, d.root, "notes.txt", "notes.txt", "text/plain", notesText)
	d.add(t, reports, "q1.pdf", "Q1 report.pdf", protonFile, 1200)
	return d
}

// remove takes a link out of a folder's listing, the way something deleted
// elsewhere leaves it: no event, and nothing but its absence to go on.
func (d *indexedTree) remove(t *testing.T, parent *cannedFolder, linkID string) {
	t.Helper()
	kept := parent.children[:0]
	for _, child := range parent.children {
		if child.(map[string]any)["LinkID"] != linkID {
			kept = append(kept, child)
		}
	}
	parent.children = kept
	d.publish(t, parent)
}

// protonFile is Proton's number for a file link, beside protonFolder.
const protonFile = 2

// cannedFolder is one folder of the canned tree: its own key, and what it answers
// with when its children are asked for.
type cannedFolder struct {
	id       string
	kr       *pgp.KeyRing
	children []any
}

func (d *indexedTree) folder(t *testing.T, id string, kr *pgp.KeyRing) *cannedFolder {
	t.Helper()
	f := &cannedFolder{id: id, kr: kr}
	d.publish(t, f)
	return f
}

// add seals a child into a folder the way Proton seals one, and publishes the
// folder's new listing and the child itself.
func (d *indexedTree) add(t *testing.T, parent *cannedFolder, id, name string, kind int, size int64) *cannedFolder {
	t.Helper()
	nodeKey, pass, passSig, priv, err := genNodeKeys(parent.kr, d.tr.addrKR)
	if err != nil {
		t.Fatalf("generate node keys: %v", err)
	}
	encName, err := encryptName(name, parent.kr, d.tr.addrKR)
	if err != nil {
		t.Fatalf("encrypt a name: %v", err)
	}
	link := map[string]any{
		"LinkID": id, "ParentLinkID": parent.id, "Type": kind, "Name": encName, "Size": size,
		"NodeKey": nodeKey, "NodePassphrase": pass, "NodePassphraseSignature": passSig,
		"ModifyTime": 1700000000,
	}
	parent.children = append(parent.children, link)
	d.doer.routes["GET /drive/shares/"+testShareID+"/links/"+id] =
		object(t, map[string]any{"Link": link})
	d.publish(t, parent)
	kr, err := pgp.NewKeyRing(priv)
	if err != nil {
		t.Fatalf("node key ring: %v", err)
	}
	child := &cannedFolder{id: id, kr: kr}
	if kind == protonFolder {
		d.publish(t, child)
	}
	return child
}

// addFile seals a file into a folder the way an upload leaves one: a session
// key under the node key, one encrypted block behind a URL, and a manifest the
// node key signed.
//
// It is what makes the second pass answerable here: what a keyword searches is
// the bytes coming back and being read as text, and nothing short of a real
// transfer proves that end of it.
func (d *indexedTree) addFile(t *testing.T, parent *cannedFolder, id, name, mimeType, content string) {
	t.Helper()
	child := d.add(t, parent, id, name, protonFile, int64(len(content)))
	sk, keyPacket, _, err := genFileKeys(child.kr)
	if err != nil {
		t.Fatalf("generate file keys: %v", err)
	}
	block, _, err := encryptBlock([]byte(content), sk, child.kr, child.kr)
	if err != nil {
		t.Fatalf("encrypt the block: %v", err)
	}
	hash := sha256.Sum256(block)
	sig, err := child.kr.SignDetached(pgp.NewPlainMessage(hash[:]))
	if err != nil {
		t.Fatalf("sign the manifest: %v", err)
	}
	manifest, err := sig.GetArmored()
	if err != nil {
		t.Fatalf("armor the manifest signature: %v", err)
	}

	const revision = "rev-1"
	d.blocks[id] = block
	link := parent.children[len(parent.children)-1].(map[string]any)
	link["MIMEType"] = mimeType
	link["FileProperties"] = map[string]any{
		"ContentKeyPacket": keyPacket,
		"ActiveRevision":   map[string]any{"ID": revision},
	}
	d.publish(t, parent)
	d.doer.routes["GET /drive/shares/"+testShareID+"/links/"+id] =
		object(t, map[string]any{"Link": link})
	d.doer.routes["GET /drive/shares/"+testShareID+"/files/"+id+"/revisions/"+revision] =
		object(t, map[string]any{"Revision": map[string]any{
			"ManifestSignature": manifest,
			"Blocks": []any{map[string]any{
				"Index": 1, "BareURL": d.storage.URL + "/" + id,
				"Token": "token", "Hash": base64.StdEncoding.EncodeToString(hash[:]),
			}},
		}})
}

// publish is what Proton answers when a folder's children are asked for.
func (d *indexedTree) publish(t *testing.T, f *cannedFolder) {
	t.Helper()
	d.doer.routes["GET /drive/shares/"+testShareID+"/folders/"+f.id+"/children"] =
		object(t, map[string]any{"Links": f.children})
}

// says puts one page of the volume's feed in front of the next sync.
func (d *indexedTree) says(t *testing.T, events ...any) {
	t.Helper()
	d.doer.routes["GET /drive/volumes/"+testVolumeID+"/events/cursor-0"] = object(t, map[string]any{
		"EventID": "cursor-1", "More": 0, "Events": events,
	})
}

func (d *indexedTree) rootKey(t *testing.T) *pgp.KeyRing {
	t.Helper()
	res, err := d.s.ResolvePath(t.Context(), d.dc, "/")
	if err != nil {
		t.Fatalf("resolve the root: %v", err)
	}
	return res.NodeKR
}

func (d *indexedTree) build(t *testing.T) search.Result {
	t.Helper()
	got, err := d.indexing(t).Build(t.Context(), progress.Nop{})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	return got
}

func (d *indexedTree) catchUp(t *testing.T) search.Result {
	t.Helper()
	got, err := d.indexing(t).Sync(t.Context())
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	return got
}

func (d *indexedTree) indexing(t *testing.T) *indexSession {
	t.Helper()
	x, err := d.s.openIndex(t.Context())
	if err != nil {
		t.Fatalf("open the index: %v", err)
	}
	return x
}

func (d *indexedTree) tree(t *testing.T) []Child {
	t.Helper()
	return d.matching(t, "")
}

// matching is what a keyword finds, wherever the tree was read from.
func (d *indexedTree) matching(t *testing.T, keyword string) []Child {
	t.Helper()
	items, _, err := d.s.Walk(t.Context(), d.dc, "/", keyword)
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	return items
}

// coverage is what a listing says a keyword was able to read.
func (d *indexedTree) coverage(t *testing.T, dc *Context) Coverage {
	t.Helper()
	_, cover, err := d.s.Walk(t.Context(), dc, "/", "anything")
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	return cover
}

func pathsOf(items []Child) map[string]string {
	out := make(map[string]string, len(items))
	for _, ch := range items {
		out[ch.LinkID] = ch.Path
	}
	return out
}

// The tree that is walked and the tree the index answers with are the same tree.
func TestTheIndexAnswersWithTheTreeTheWalkAnswersWith(t *testing.T) {
	d := newIndexedTree(t)
	walked := pathsOf(d.tree(t))
	if len(walked) != 4 {
		t.Fatalf("the walk found %d items, want the 4 in the tree", len(walked))
	}

	d.build(t)
	fromIndex := pathsOf(d.tree(t))

	if len(fromIndex) != len(walked) {
		t.Fatalf("the index answered with %d items, the walk with %d", len(fromIndex), len(walked))
	}
	for id, path := range walked {
		if fromIndex[id] != path {
			t.Errorf("%s is at %q in the index and %q in the walk", id, fromIndex[id], path)
		}
	}
	if fromIndex["q1.pdf"] != "/Reports/Q1 report.pdf" {
		t.Errorf("a nested file is at %q", fromIndex["q1.pdf"])
	}
}

// A folder that was renamed takes everything under it with it, which the feed
// reports about the folder and not about its contents.
func TestARenamedFolderTakesItsContentsWithIt(t *testing.T) {
	d := newIndexedTree(t)
	d.build(t)

	renamed, err := encryptName("Archive", d.rootKey(t), d.tr.addrKR)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	d.says(t, map[string]any{
		"EventType": eventRename,
		"Link": map[string]any{
			"LinkID": "folder-1", "ParentLinkID": testRootID, "Type": protonFolder,
			"Name": renamed, "ModifyTime": 1700000001,
		},
	})
	d.catchUp(t)

	paths := pathsOf(d.tree(t))
	if paths["folder-1"] != "/Archive" {
		t.Errorf("the folder is at %q, want /Archive", paths["folder-1"])
	}
	if paths["q1.pdf"] != "/Archive/Q1 report.pdf" {
		t.Errorf("the file inside it is at %q, want it to follow the folder", paths["q1.pdf"])
	}
}

// A file the account no longer has leaves the tree, and so does one in the
// trash - the trash is a listing of its own.
func TestWhatLeavesTheAccountLeavesTheTree(t *testing.T) {
	d := newIndexedTree(t)
	d.build(t)

	trashed, err := encryptName("invoice.pdf", d.rootKey(t), d.tr.addrKR)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	d.says(t,
		map[string]any{"EventType": eventDelete, "Link": map[string]any{"LinkID": "notes.txt"}},
		map[string]any{"EventType": eventUpdate, "Link": map[string]any{
			"LinkID": "invoice.pdf", "ParentLinkID": testRootID, "Type": protonFile,
			"Name": trashed, "Size": 312, "Trashed": 1700000002,
		}},
	)
	if got := d.catchUp(t); got.Removed != 1 || got.Indexed != 1 {
		t.Errorf("sync = %+v, want one removed and one rewritten", got)
	}

	paths := pathsOf(d.tree(t))
	if _, there := paths["notes.txt"]; there {
		t.Error("a deleted file is still in the tree")
	}
	if _, there := paths["invoice.pdf"]; there {
		t.Error("a trashed file is still in the tree")
	}
	if _, there := paths["q1.pdf"]; !there {
		t.Error("the rest of the tree went with it")
	}
}

// A file added after the build is in the account, so it is in the answer: the
// feed is applied before the tree is read.
func TestAFileAddedAfterTheBuildIsInTheTree(t *testing.T) {
	d := newIndexedTree(t)
	d.build(t)

	name, err := encryptName("receipt.pdf", d.rootKey(t), d.tr.addrKR)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	d.says(t, map[string]any{
		"EventType": eventCreate,
		"Link": map[string]any{
			"LinkID": "receipt.pdf", "ParentLinkID": testRootID, "Type": protonFile,
			"Name": name, "Size": 99, "ModifyTime": 1700000003,
		},
	})

	paths := pathsOf(d.tree(t))
	if paths["receipt.pdf"] != "/receipt.pdf" {
		t.Errorf("a file added after the build is at %q, want /receipt.pdf", paths["receipt.pdf"])
	}
}

// A tree that is not the account's own is walked whatever is indexed: the index
// holds one volume, and answering for another from it would be answering about
// the wrong tree.
//
// A keyword there reads names alone, and the listing is told so rather than
// left looking like a search of what the files say.
func TestATreeThatIsNotTheAccountsIsWalked(t *testing.T) {
	d := newIndexedTree(t)
	d.build(t)

	elsewhere := *d.dc
	elsewhere.VolumeID = "another-volume"
	if _, _, ok := d.s.indexedTree(t.Context(), &elsewhere, "/", nil); ok {
		t.Error("the index answered for a volume it was not built from")
	}
	if _, _, ok := d.s.indexedTree(t.Context(), nil, "/", nil); ok {
		t.Error("the index answered for no tree at all")
	}

	computer := *d.dc
	computer.Type = shareTypeDevice
	if cover := d.coverage(t, &computer); cover.Indexed || !cover.Foreign {
		t.Errorf("a computer's tree reported %+v, want it named as somewhere the index does not cover", cover)
	}
}

// An account whose files moved to another volume has a different tree, and the
// index follows it there: what was indexed goes, the new tree is read, and the
// catch-ups after that follow the new volume's history rather than asking the
// old one for ever.
func TestAnIndexFollowsTheAccountToANewVolume(t *testing.T) {
	d := newIndexedTree(t)
	d.build(t)

	const moved = "volume-2"
	d.doer.routes["GET /drive/volumes"] = volumeList(`{"VolumeID":"` + moved + `","State":1,"Type":1,` +
		`"Share":{"ShareID":"` + testShareID + `","LinkID":"` + testRootID + `"}}`)
	d.doer.routes["GET /drive/volumes/"+moved+"/events/latest"] = `{"EventID":"moved-0"}`
	d.doer.routes["GET /drive/volumes/"+moved+"/events/moved-0"] = `{"EventID":"moved-1","More":0,"Events":[]}`

	// What one run does: build, catch up, and read again where the catch-up says
	// the index owes a reading.
	x := d.indexing(t)
	if _, err := x.Build(t.Context(), progress.Nop{}); err != nil {
		t.Fatalf("build: %v", err)
	}
	got, err := x.Sync(t.Context())
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if !got.Refreshed || got.Removed != 4 {
		t.Fatalf("sync = %+v, want the 4 items of the old tree removed and a reading owed", got)
	}
	if again, err := x.Build(t.Context(), progress.Nop{}); err != nil || again.Indexed != 4 {
		t.Fatalf("reading the new tree = %+v, %v, want its 4 items indexed", again, err)
	}
	after, err := x.Sync(t.Context())
	if err != nil {
		t.Fatalf("sync after the reading: %v", err)
	}
	if after.Refreshed {
		t.Error("the index still owes a reading after the new tree was read")
	}

	st := x.Status()
	if st.Volume != moved || !st.Complete || st.Stale || st.Indexed != 4 {
		t.Errorf("status = %+v, want a whole index of the new volume", st)
	}
	if !d.doer.sent("GET", "/drive/volumes/"+moved+"/events/moved-0") {
		t.Error("the catch-up did not follow the new volume's history")
	}
}

// What is written down reads back as the item it was.
func TestWhatIsIndexedReadsBackAsTheItemItWas(t *testing.T) {
	in := stored{LinkID: "a", ParentID: "b", Name: "invoice.pdf", Type: TypeFile, Size: 312}
	rec, err := record(in)
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	var back stored
	if err := json.Unmarshal(rec.Data, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back != in {
		t.Errorf("read back %+v, want %+v", back, in)
	}
	got := back.child("/invoice.pdf")
	if got.Path != "/invoice.pdf" || got.Name != in.Name || got.Size != in.Size || got.Type != in.Type {
		t.Errorf("as a listing row = %+v", got)
	}
}

// A file that arrives in the same batch as the folder it is in is in that
// folder, which is what uploading a directory produces.
func TestAFileCreatedWithItsFolderIsInIt(t *testing.T) {
	d := newIndexedTree(t)
	d.build(t)

	photos := d.add(t, d.root, "folder-2", "Photos", protonFolder, 0)
	d.add(t, photos, "cat.jpg", "cat.jpg", protonFile, 4096)
	folderLink := d.root.children[len(d.root.children)-1]
	fileLink := photos.children[len(photos.children)-1]
	d.doer.routes["GET /drive/shares/"+testShareID+"/links/folder-2"] =
		object(t, map[string]any{"Link": folderLink})

	d.says(t,
		map[string]any{"EventType": eventCreate, "Link": folderLink},
		map[string]any{"EventType": eventCreate, "Link": fileLink},
	)
	d.catchUp(t)

	paths := pathsOf(d.tree(t))
	if paths["cat.jpg"] != "/Photos/cat.jpg" {
		t.Errorf("the file is at %q, want it inside the folder it arrived with", paths["cat.jpg"])
	}
}

// A folder in the trash takes everything under it out of the tree, and so does
// one that is deleted outright. Proton reports the folder and nothing else.
func TestWhatHappensToAFolderHappensToWhatIsInIt(t *testing.T) {
	for _, tc := range []struct {
		name  string
		event map[string]any
	}{
		{name: "trashed"},
		{name: "deleted", event: map[string]any{
			"EventType": eventDelete, "Link": map[string]any{"LinkID": "folder-1"},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := newIndexedTree(t)
			d.build(t)
			event := tc.event
			if event == nil {
				name, err := encryptName("Reports", d.rootKey(t), d.tr.addrKR)
				if err != nil {
					t.Fatalf("encrypt: %v", err)
				}
				event = map[string]any{"EventType": eventUpdate, "Link": map[string]any{
					"LinkID": "folder-1", "ParentLinkID": testRootID, "Type": protonFolder,
					"Name": name, "Trashed": 1700000002,
				}}
			}
			d.says(t, event)
			d.catchUp(t)

			paths := pathsOf(d.tree(t))
			if _, there := paths["folder-1"]; there {
				t.Error("the folder is still in the tree")
			}
			if _, there := paths["q1.pdf"]; there {
				t.Errorf("the file inside it is still in the tree, at %q", paths["q1.pdf"])
			}
			if _, there := paths["invoice.pdf"]; !there {
				t.Error("the rest of the tree went with it")
			}
		})
	}
}

// A build over an unchanged tree writes nothing: what is in the index is what
// the walk found, item by item.
func TestReadingTheTreeAgainWritesOnlyWhatChanged(t *testing.T) {
	d := newIndexedTree(t)
	if got := d.build(t); got.Indexed != 4 {
		t.Fatalf("the first build = %+v, want the 4 items of the tree", got)
	}

	// What a feed that gave up leaves behind: the index is read against the
	// account again rather than followed.
	x := d.indexing(t)
	x.log.State.Stale = true
	got, err := x.Build(t.Context(), progress.Nop{})
	if err != nil {
		t.Fatalf("read again: %v", err)
	}
	if got.Indexed != 0 || got.Removed != 0 {
		t.Errorf("reading an unchanged tree again = %+v, want nothing written", got)
	}
	if st := x.Status(); st.Stale || !st.Complete || st.Indexed != 4 {
		t.Errorf("status = %+v, want a whole index of 4 items", st)
	}
}

// A file that left the volume while nothing was following it leaves the index
// too: reading the tree is what establishes that it is gone.
func TestWhatALaterReadingDoesNotFindLeavesTheIndex(t *testing.T) {
	d := newIndexedTree(t)
	d.build(t)

	d.remove(t, d.root, "notes.txt")

	x := d.indexing(t)
	x.log.State.Stale = true
	got, err := x.Build(t.Context(), progress.Nop{})
	if err != nil {
		t.Fatalf("read again: %v", err)
	}
	if got.Removed != 1 {
		t.Errorf("reading the tree again = %+v, want the file that is gone removed", got)
	}
	if _, there := pathsOf(d.tree(t))["notes.txt"]; there {
		t.Error("a file the account no longer has is still in the tree")
	}
}

// ── what a file says ──

// A keyword reads what a file says as well as what it is called, which is the
// whole point of holding the text: Proton can answer neither.
//
// The type is half of it. A file that is not text is not searched for words it
// does not have, however its bytes happen to read - so the photograph here
// carries the same phrase as the notes and is not an answer to it.
func TestAKeywordReadsNamesAndWhatFilesSay(t *testing.T) {
	d := newIndexedTree(t)
	d.addFile(t, d.root, "holiday.png", "holiday.png", "image/png", notesText)
	d.build(t)

	byText := d.matching(t, "parking permit")
	if len(byText) != 1 || byText[0].LinkID != "notes.txt" {
		t.Fatalf("a keyword inside a file matched %+v, want notes.txt alone", pathsOf(byText))
	}
	byName := d.matching(t, "invoice")
	if len(byName) != 1 || byName[0].LinkID != "invoice.pdf" {
		t.Fatalf("a keyword in a name matched %+v, want invoice.pdf alone", pathsOf(byName))
	}
	if got := d.matching(t, "glovebox Q1"); len(got) != 0 {
		t.Errorf("a keyword whose terms are in different items matched %+v, want nothing", pathsOf(got))
	}
	if got := d.matching(t, "GLOVEBOX"); len(got) != 1 {
		t.Errorf("a keyword in another case matched %+v, want the file that says it", pathsOf(got))
	}
}

// A listing says how much of the text it searched had been read, so an empty
// answer from a half-built index cannot be read as "no such file".
//
// A file whose transfer failed is what leaves one half-built: it keeps its
// place in the index, the listing says the answer is short, and the next build
// asks for it again rather than writing it off.
func TestAListingSaysHowMuchOfTheTextItRead(t *testing.T) {
	d := newIndexedTree(t)
	d.addFile(t, d.root, "recipe.md", "recipe.md", "text/markdown", "salt, flour, water")
	block := d.blocks["recipe.md"]
	delete(d.blocks, "recipe.md")

	d.build(t)
	cover := d.coverage(t, d.dc)
	if !cover.Indexed || cover.Texts != 2 || cover.Read != 1 {
		t.Fatalf("coverage = %+v, want the index answering and owing one of two texts", cover)
	}
	if !cover.Short() {
		t.Error("a listing whose texts are not all read says it read them all")
	}
	if got := d.matching(t, "flour"); len(got) != 0 {
		t.Errorf("a keyword matched %+v from a file whose text never arrived", pathsOf(got))
	}

	d.blocks["recipe.md"] = block
	d.build(t)
	if cover := d.coverage(t, d.dc); cover.Read != 2 || cover.Short() {
		t.Errorf("coverage = %+v, want every text read", cover)
	}
	if got := d.matching(t, "flour"); len(got) != 1 {
		t.Errorf("a keyword matched %+v, want the file whose text arrived on the second run", pathsOf(got))
	}
}

// A file the index could not read is held by everything else it says about
// itself, and counted where a listing of indexes shows it.
//
// The alternative is a drive where a file is in a listing and not in a search,
// which is the shape of a search nobody can trust.
func TestAFileWhoseBytesAreNotTextIsHeldByItsName(t *testing.T) {
	d := newIndexedTree(t)
	// A name and a type that promise text, over bytes that are not: what a
	// client stored beside the file is whatever it made of the name.
	d.addFile(t, d.root, "photo.txt", "photo.txt", "text/plain", "\xff\xfe not text at all")
	got := d.build(t)
	if got.Unreadable != 1 {
		t.Errorf("the build = %+v, want the one file whose bytes would not read counted", got)
	}

	x := d.indexing(t)
	if st := x.Status(); st.Unreadable != 1 || st.Bodies != st.Texts {
		t.Errorf("status = %+v, want the file counted as unreadable and nothing still owed", st)
	}
	if found := d.matching(t, "photo.txt"); len(found) != 1 {
		t.Errorf("a keyword matching the name found %+v, want the file itself", pathsOf(found))
	}
}

// Which files the second pass owes is the candidate rule, and it is the whole
// of what a build spends: a file too large, a file in the trash and a file
// that is not text are not downloaded at all.
func TestWhatTheIndexOwesAText(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   stored
		want bool
	}{
		{
			name: "a text file",
			in:   stored{Type: TypeFile, Name: "notes.md", Size: 4200},
			want: true,
		},
		{name: "a folder", in: stored{Type: TypeFolder, Name: "Notes", Size: 0}},
		{name: "a photograph", in: stored{Type: TypeFile, Name: "holiday.jpg", Size: 4200}},
		{
			name: "a text file in the trash",
			in:   stored{Type: TypeFile, Name: "notes.md", Size: 4200, Trashed: 1700000000},
		},
		{
			name: "more text than the index keeps",
			in:   stored{Type: TypeFile, Name: "export.csv", Size: maxTextSize + 1},
		},
		{
			name: "as much text as the index keeps",
			in:   stored{Type: TypeFile, Name: "export.csv", Size: maxTextSize},
			want: true,
		},
		{name: "an empty file", in: stored{Type: TypeFile, Name: "notes.md"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.in.wantsText(); got != tc.want {
				t.Errorf("wantsText = %v, want %v", got, tc.want)
			}
			if tc.want && tc.in.settled() {
				t.Error("a file whose text is wanted and not held reads as settled")
			}
		})
	}
}

// A file uploaded over is a different file to search, so the text the index
// holds for the version before it is not carried across.
func TestANewVersionOwesItsTextAgain(t *testing.T) {
	held := stored{
		LinkID: "notes.txt", Type: TypeFile, Name: "notes.txt", Size: 12,
		Revision: "rev-1", Text: "the old text", Fetched: true,
	}
	same := stored{LinkID: "notes.txt", Type: TypeFile, Name: "notes.txt", Size: 12, Revision: "rev-1"}
	if got := same.texted(held, true); got.Text != "the old text" || !got.Fetched {
		t.Errorf("the same version = %+v, want the text the index already holds", got)
	}
	newer := stored{LinkID: "notes.txt", Type: TypeFile, Name: "notes.txt", Size: 14, Revision: "rev-2"}
	got := newer.texted(held, true)
	if got.Text != "" || got.Fetched || got.settled() {
		t.Errorf("a new version = %+v, want it owed its text again", got)
	}
}
