package drive

import (
	"context"
	"encoding/json"
	"testing"

	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
	"github.com/roman-16/proton-cli/internal/progress"
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
}

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

	d := &indexedTree{s: s, tr: tr, doer: doer}
	dc, err := s.Resolve(t.Context())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	d.dc = dc

	root := d.folder(t, testRootID, d.rootKey(t))
	reports := d.add(t, root, "folder-1", "Reports", protonFolder, 0)
	d.add(t, root, "invoice.pdf", "invoice.pdf", protonFile, 312)
	d.add(t, root, "notes.txt", "notes.txt", protonFile, 12)
	d.add(t, reports, "q1.pdf", "Q1 report.pdf", protonFile, 1200)
	return d
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
// folder's new listing.
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
	parent.children = append(parent.children, map[string]any{
		"LinkID": id, "ParentLinkID": parent.id, "Type": kind, "Name": encName, "Size": size,
		"NodeKey": nodeKey, "NodePassphrase": pass, "NodePassphraseSignature": passSig,
		"ModifyTime": 1700000000,
	})
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

func (d *indexedTree) build(t *testing.T) {
	t.Helper()
	if _, err := d.s.buildIndex(t.Context(), progress.Nop{}); err != nil {
		t.Fatalf("build: %v", err)
	}
}

func (d *indexedTree) tree(t *testing.T) []Child {
	t.Helper()
	items, err := d.s.Walk(t.Context(), d.dc, "/")
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	return items
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
	if _, err := d.s.syncIndex(t.Context()); err != nil {
		t.Fatalf("sync: %v", err)
	}

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
	got, err := d.s.syncIndex(t.Context())
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if got.Removed != 1 || got.Indexed != 1 {
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
func TestATreeThatIsNotTheAccountsIsWalked(t *testing.T) {
	d := newIndexedTree(t)
	d.build(t)

	elsewhere := *d.dc
	elsewhere.VolumeID = "another-volume"
	if _, ok := d.s.indexedTree(t.Context(), &elsewhere, "/"); ok {
		t.Error("the index answered for a volume it was not built from")
	}
	if _, ok := d.s.indexedTree(t.Context(), nil, "/"); ok {
		t.Error("the index answered for no tree at all")
	}
}

// What is written down reads back as the item it was.
func TestWhatIsIndexedReadsBackAsTheItemItWas(t *testing.T) {
	in := stored{LinkID: "a", ParentID: "b", Name: "invoice.pdf", Path: "/invoice.pdf", Type: TypeFile, Size: 312}
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
	if got := back.child(); got.Path != in.Path || got.Size != in.Size || got.Type != in.Type {
		t.Errorf("as a listing row = %+v", got)
	}
}
