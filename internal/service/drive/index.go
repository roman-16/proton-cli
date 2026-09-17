package drive

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
	"github.com/roman-16/proton-cli/internal/progress"
	"github.com/roman-16/proton-cli/internal/proton"
	"github.com/roman-16/proton-cli/internal/search"
	"github.com/roman-16/proton-cli/internal/skip"
)

// Indexing Drive is holding the tree, so a question about it stops being a walk.
//
// Nothing in Drive is searchable at Proton: a file's name is encrypted to the
// folder above it, so finding one means opening every folder on the way. That is
// what a filtered listing does today - one request per folder, every time - and
// what an index turns into one request that asks what has changed.
//
// What it holds is the shape of the tree: names, sizes, times, and which folder
// each thing hangs in. Not where it sits, which is the folders' to say and is
// worked out when the tree is read - a folder that is renamed, moved or trashed
// takes everything under it with it, and the feed reports the folder alone. Not
// the contents of a file, which are what Drive is mostly made of, and not the
// keys that open them: a run that has to decrypt a name fetches the folder's key
// again rather than keeping it.

// stored is one link as the index holds it: what a listing shows, and the
// parent it hangs from.
type stored struct {
	LinkID     string `json:"link_id"`
	ParentID   string `json:"parent_id,omitempty"`
	Name       string `json:"name"`
	Type       string `json:"type"`
	Size       int64  `json:"size,omitempty"`
	CreateTime int64  `json:"create_time,omitempty"`
	ModifyTime int64  `json:"modify_time,omitempty"`
	// Trashed is when the item was put in the trash. A trashed item is in the
	// account but not in the tree, so it is held and left out of a listing.
	Trashed int64 `json:"trashed,omitempty"`
	// Unreadable says the name would not decrypt with this account's keys, so
	// the item is held by everything else it says about itself.
	Unreadable bool `json:"unreadable,omitempty"`
}

func (in stored) child(path string) Child {
	return Child{
		LinkID: in.LinkID, Name: displayName(in.Name, in.Unreadable), Path: path,
		Type: in.Type, Size: in.Size, CreateTime: in.CreateTime, ModifyTime: in.ModifyTime,
	}
}

// SetIndex hands the service the profile's index directory.
func (s *Service) SetIndex(store *search.Store) { s.index = store }

// IndexPart is what `index` drives to keep the Drive index current.
func (s *Service) IndexPart() search.Part { return indexPart{s: s} }

type indexPart struct{ s *Service }

func (indexPart) App() search.App { return search.AppDrive }
func (indexPart) Noun() string    { return "items" }

func (p indexPart) Open(ctx context.Context) (search.Session, error) { return p.s.openIndex(ctx) }

// Count is how many items a build would index, which is the walk it would do.
//
// There is no cheaper answer: how many things are in the tree is the thing the
// walk finds out, and Proton has nothing that says so. A preview therefore costs
// what the reading half of a build costs, and writes nothing.
func (p indexPart) Count(ctx context.Context) (int, error) {
	dc, err := p.s.Resolve(ctx)
	if err != nil {
		return 0, err
	}
	count := 0
	_, err = p.s.walkFiles(ctx, dc, func(found) error {
		count++
		return nil
	})
	return count, err
}

// indexSession is the Drive index, open.
type indexSession struct {
	s   *Service
	log *search.Log
	dc  *Context
}

func (s *Service) openIndex(ctx context.Context) (*indexSession, error) {
	log, err := s.index.Load(ctx, search.AppDrive)
	if err != nil {
		return nil, err
	}
	return &indexSession{s: s, log: log}, nil
}

func (x *indexSession) Status() search.Status { return x.log.Status() }

// context is the account's own volume and share, looked up once a run needs it.
func (x *indexSession) context(ctx context.Context) (*Context, error) {
	if x.dc != nil {
		return x.dc, nil
	}
	dc, err := x.s.Resolve(ctx)
	if err != nil {
		return nil, err
	}
	x.dc = dc
	return dc, nil
}

func (x *indexSession) save() error { return x.log.Save(time.Now().Unix()) }

// Build walks the whole tree and writes down what it found.
//
// It is one pass rather than a resumable one: a walk is depth-first through
// folders whose keys are opened on the way, so there is no anchor to carry on
// from that is cheaper than starting again. What it costs is one request per
// folder, which is what a single filtered listing costs today.
//
// It runs again when Proton says it cannot describe what has happened to the
// volume. Then the walk is the answer: what came back is the tree, what did not
// is no longer in it, and neither is a thing the feed could have said.
func (x *indexSession) Build(ctx context.Context, sink progress.Sink) (search.Result, error) {
	log := x.log
	if log.State.Complete && !log.State.Stale {
		return search.Result{}, nil
	}
	dc, err := x.context(ctx)
	if err != nil {
		return search.Result{}, err
	}
	// The cursor is taken before the walk, so a file uploaded while it runs is
	// caught by the first sync rather than missed by both.
	if log.State.Cursor == "" {
		cursor, err := x.s.latestVolumeEvent(ctx, dc.VolumeID)
		if err != nil {
			return search.Result{}, err
		}
		log.State.Cursor = cursor
		log.State.Volume = dc.VolumeID
		if err := x.save(); err != nil {
			return search.Result{}, err
		}
	}

	// How many there are is what the walk is finding out, so the bar counts up
	// rather than towards anything and closes when the walk ends.
	progress.Counting(sink, "items")
	sink.Start(0, "Indexing drive")
	var done search.Result
	var records []search.Record
	seen := make(map[string]bool, log.State.Indexed)
	unreadable := 0
	tally, err := x.s.walkFiles(ctx, dc, func(f found) error {
		seen[f.Link.LinkID] = true
		if f.Unreadable {
			unreadable++
		}
		sink.Add(1)
		before, had := held(log, f.Link.LinkID)
		in := stored{
			LinkID: f.Link.LinkID, ParentID: f.Link.ParentLinkID, Name: f.Name,
			Type: linkType(f.Link.Type), Size: f.Link.Size, CreateTime: f.Link.CreateTime,
			ModifyTime: f.Link.ModifyTime, Trashed: f.Link.Trashed, Unreadable: f.Unreadable,
		}
		if had && before == in {
			return nil
		}
		rec, err := record(in)
		if err != nil {
			return err
		}
		records = append(records, rec)
		done.Indexed++
		if len(records) < indexBatch {
			return nil
		}
		if err := log.Append(records...); err != nil {
			return err
		}
		records = records[:0]
		return x.save()
	})
	if err != nil {
		return done, err
	}
	if err := log.Append(records...); err != nil {
		return done, err
	}
	sink.Done()

	// A walk that could not read part of the tree has not established that what
	// it did not see is gone, so the index keeps it and says it is unfinished.
	if tally.whole() {
		gone, err := x.tombstoneUnseen(seen)
		done = search.Total(done, gone)
		if err != nil {
			return done, err
		}
		if gone.Removed > 0 {
			slog.DebugContext(ctx, "drive: items left the volume while nothing was following it",
				"kind", string(skip.KindVolume), "reason", string(skip.Unreadable), "count", gone.Removed)
		}
	}
	done.Unreadable = unreadable + tally.Sealed
	log.State.Unreadable = done.Unreadable
	log.State.Total = log.State.Indexed
	log.State.Complete = tally.whole()
	log.State.Stale = !tally.whole()
	return done, x.save()
}

// tombstoneUnseen takes out everything a reading of the tree did not come
// across - and everything there is, when nothing was seen.
func (x *indexSession) tombstoneUnseen(seen map[string]bool) (search.Result, error) {
	var done search.Result
	var records []search.Record
	for _, rec := range x.log.Records() {
		if seen[rec.ID] {
			continue
		}
		records = append(records, search.Record{ID: rec.ID, Gone: true})
		done.Removed++
	}
	return done, x.log.Append(records...)
}

// indexBatch is how many records one write carries. A build of a large tree
// would otherwise hold the whole of it before writing any of it, and lose the
// lot when somebody stops it.
const indexBatch = 200

// walkFiles walks the account's own file tree from its root.
func (s *Service) walkFiles(ctx context.Context, dc *Context, visit func(found) error) (walked, error) {
	res, err := s.ResolvePath(ctx, dc, "/")
	if err != nil {
		return walked{}, err
	}
	return s.walkTree(ctx, dc, res.LinkID, res.NodeKR, "", visit)
}

// Sync applies the volume's change feed.
//
// Proton reports a link as it now is, so a file that was renamed, moved,
// trashed or restored arrives as the whole record and is written down again.
// What the event cannot carry is the name in the clear, so the folder it hangs
// in is opened to read it - the one place indexing Drive costs keys.
func (x *indexSession) Sync(ctx context.Context) (search.Result, error) {
	log := x.log
	if log.State.Cursor == "" {
		return search.Result{}, nil
	}
	dc, err := x.context(ctx)
	if err != nil {
		return search.Result{}, err
	}
	if log.State.Volume != "" && log.State.Volume != dc.VolumeID {
		return x.forgetVolume(ctx)
	}

	names := &naming{s: x.s, dc: dc, keys: map[string]*pgp.KeyRing{}}
	var done search.Result
	for page := 0; page < maxDrain; page++ {
		var batch volumeEvents
		if err := x.s.C.Decode(ctx, proton.Request{
			Method: "GET",
			Path:   fmt.Sprintf("/drive/volumes/%s/events/%s", dc.VolumeID, log.State.Cursor),
		}, &batch); err != nil {
			return done, err
		}
		if batch.Refresh != 0 {
			slog.WarnContext(ctx, "The Drive index missed part of the volume's history and will be read from Proton again.",
				"kind", string(skip.KindVolume), "reason", string(skip.Unreadable))
			log.State.Stale = true
			cursor, err := x.s.latestVolumeEvent(ctx, dc.VolumeID)
			if err != nil {
				return done, err
			}
			log.State.Cursor = cursor
			done.Refreshed = true
			return done, x.save()
		}
		applied, err := x.applyEvents(ctx, names, batch)
		done = search.Total(done, applied)
		if err != nil {
			return done, err
		}
		// The cursor moves once the page is in the index, so a page that could not
		// be applied is asked for again rather than skipped - by the next run, or by
		// the next poll of a watch that keeps this session open.
		log.State.Cursor = batch.EventID
		if batch.More == 0 {
			break
		}
	}
	return done, x.save()
}

// forgetVolume empties an index whose volume is not the one the account's files
// are on any more.
//
// A different volume is a different tree altogether, with a history of its own:
// what is indexed says nothing about it, and the feed that was being followed
// does not describe it. So the index is left owing a reading of the account with
// no cursor and no volume, which is what has the build that follows read the
// new tree and take a cursor into its history.
func (x *indexSession) forgetVolume(ctx context.Context) (search.Result, error) {
	slog.WarnContext(ctx, "The account's files are on a different volume from the one the Drive index was built from, so it will be read from Proton again.",
		"kind", string(skip.KindVolume), "reason", string(skip.Unreadable))
	done, err := x.tombstoneUnseen(nil)
	if err != nil {
		return done, err
	}
	log := x.log
	log.State.Cursor, log.State.Volume, log.State.Unreadable = "", "", 0
	log.State.Stale = true
	done.Refreshed = true
	return done, x.save()
}

// maxDrain caps how many pages one catch-up follows, so a long backlog cannot
// hold the run indefinitely.
const maxDrain = 50

// The volume feed's event types, as Proton numbers them.
const (
	eventDelete = 0
	eventCreate = 1
	eventUpdate = 2
	eventRename = 3
)

type volumeEvents struct {
	EventID string
	More    int
	Refresh int
	Events  []struct {
		EventType int
		Link      Link
	}
}

func (s *Service) latestVolumeEvent(ctx context.Context, volumeID string) (string, error) {
	var r struct{ EventID string }
	if err := s.C.Decode(ctx, proton.Request{
		Method: "GET", Path: fmt.Sprintf("/drive/volumes/%s/events/latest", volumeID),
	}, &r); err != nil {
		return "", err
	}
	return r.EventID, nil
}

// applyEvents writes one page of changes into the index.
func (x *indexSession) applyEvents(ctx context.Context, names *naming, batch volumeEvents) (search.Result, error) {
	log := x.log
	var done search.Result
	var records []search.Record
	var below *descendants
	for _, e := range batch.Events {
		before, had := held(log, e.Link.LinkID)
		if e.EventType == eventDelete {
			records = append(records, search.Record{ID: e.Link.LinkID, Gone: true})
			done.Removed++
			if had && before.Unreadable {
				log.State.Unreadable--
			}
			// Proton reports the item that was deleted and says nothing about what
			// was inside it, so a folder takes its contents out of the index here
			// or they stay in it for good.
			if before.Type == TypeFolder {
				if below == nil {
					below = childrenOf(ctx, log)
				}
				for _, id := range below.under(e.Link.LinkID) {
					records = append(records, search.Record{ID: id, Gone: true})
					done.Removed++
				}
			}
			continue
		}
		if e.EventType != eventCreate && e.EventType != eventUpdate && e.EventType != eventRename {
			continue
		}
		in := stored{
			LinkID: e.Link.LinkID, ParentID: e.Link.ParentLinkID, Type: linkType(e.Link.Type),
			Size: e.Link.Size, CreateTime: e.Link.CreateTime, ModifyTime: e.Link.ModifyTime,
			Trashed: e.Link.Trashed,
		}
		name, err := names.name(ctx, e.Link)
		if err != nil {
			// Recorded and counted where it shows: the item goes into the index by
			// everything but its name, which is what a walk would have shown as
			// well, and `index list` counts it under what it could not read.
			slog.DebugContext(ctx, "drive: a changed item's name could not be read",
				"kind", string(skip.KindFolder), "reason", string(skip.Unlockable),
				"link", e.Link.LinkID, "parent", e.Link.ParentLinkID, "error", err)
			in.Unreadable = true
		}
		in.Name = name
		if had && before == in {
			continue
		}
		rec, err := record(in)
		if err != nil {
			return done, err
		}
		records = append(records, rec)
		done.Indexed++
		wasUnreadable := had && before.Unreadable
		switch {
		case in.Unreadable && !wasUnreadable:
			log.State.Unreadable++
		case !in.Unreadable && wasUnreadable:
			log.State.Unreadable--
		}
	}
	return done, log.Append(records...)
}

// descendants is which links hang under which, for the one question the feed
// cannot answer: what was inside a folder that is gone.
type descendants struct{ byParent map[string][]string }

func childrenOf(ctx context.Context, log *search.Log) *descendants {
	d := &descendants{byParent: map[string][]string{}}
	for _, in := range indexedItems(ctx, log) {
		d.byParent[in.ParentID] = append(d.byParent[in.ParentID], in.LinkID)
	}
	return d
}

// under is every link below one, however deep.
func (d *descendants) under(linkID string) []string {
	var out []string
	queue := append([]string{}, d.byParent[linkID]...)
	for len(queue) > 0 && len(out) <= maxTree {
		id := queue[0]
		queue = queue[1:]
		out = append(out, id)
		queue = append(queue, d.byParent[id]...)
	}
	return out
}

// maxTree is more items than a folder anybody keeps holds, and a bound on a
// cascade that a cycle in the records could otherwise spin in.
const maxTree = 1 << 20

// naming opens the folders a run has to read a name out of, once each.
type naming struct {
	s    *Service
	dc   *Context
	keys map[string]*pgp.KeyRing
}

// name is a link's name in the clear, which needs the key of the folder holding
// it.
func (f *naming) name(ctx context.Context, l Link) (string, error) {
	kr, err := f.of(ctx, l.ParentLinkID)
	if err != nil {
		return "", err
	}
	return decryptName(l.Name, kr)
}

// of is a folder's own key, found by opening every folder from the root down to
// it. A tree is deep in tens rather than thousands, and every ancestor of the
// next changed item is already open by then.
func (f *naming) of(ctx context.Context, linkID string) (*pgp.KeyRing, error) {
	if linkID == "" {
		return nil, fmt.Errorf("drive: a link with no parent has no name to read")
	}
	if kr, ok := f.keys[linkID]; ok {
		return kr, nil
	}
	if linkID == f.dc.RootLinkID {
		res, err := f.s.ResolvePath(ctx, f.dc, "/")
		if err != nil {
			return nil, err
		}
		f.keys[linkID] = res.NodeKR
		return res.NodeKR, nil
	}
	link, err := f.s.getLink(ctx, f.dc.ShareID, linkID)
	if err != nil {
		return nil, err
	}
	parentKR, err := f.of(ctx, link.ParentLinkID)
	if err != nil {
		return nil, err
	}
	kr, err := unlockNode(link, parentKR, nil)
	if err != nil {
		return nil, err
	}
	f.keys[linkID] = kr
	return kr, nil
}

// indexedItems is everything the index holds, by link.
//
// A record that will not read back is one item missing from whatever is about
// to be answered, so it is counted rather than passed over: a tree that is
// quietly short is a wrong answer that looks like a right one.
func indexedItems(ctx context.Context, log *search.Log) map[string]stored {
	out := make(map[string]stored, len(log.Records()))
	for _, rec := range log.Records() {
		var in stored
		if err := json.Unmarshal(rec.Data, &in); err != nil {
			skip.Record(ctx, skip.KindFolder, rec.ID, skip.Malformed, err)
			continue
		}
		out[in.LinkID] = in
	}
	return out
}

// held is what the index holds about one link.
func held(log *search.Log, linkID string) (stored, bool) {
	rec, ok := log.Get(linkID)
	if !ok {
		return stored{}, false
	}
	var in stored
	if err := json.Unmarshal(rec.Data, &in); err != nil {
		return stored{}, false
	}
	return in, true
}

// pathTo is where an item sits, and whether it is in the tree at all.
//
// The path is the parents': a folder that is renamed or moved changes the path
// of everything under it, and the feed reports the folder alone - so working it
// out here is what makes one event about one folder true of the whole subtree,
// with nothing to rewrite and nothing to get out of step.
//
// The trash is the same shape. An item in it is in the account and not in the
// tree, and so is everything under it, which no event ever says.
func pathTo(held map[string]stored, memo map[string]string, linkID, rootID string, depth int) (string, bool) {
	if path, worked := memo[linkID]; worked {
		return path, path != ""
	}
	in, ok := held[linkID]
	if !ok || depth > maxDepth || in.Trashed != 0 {
		return "", false
	}
	above := ""
	if in.ParentID != rootID {
		parent, ok := pathTo(held, memo, in.ParentID, rootID, depth+1)
		if !ok {
			memo[linkID] = ""
			return "", false
		}
		above = parent
	}
	path := above + "/" + displayName(in.Name, in.Unreadable)
	memo[linkID] = path
	return path, true
}

// maxDepth is deeper than any tree a person keeps and shallow enough to stop a
// cycle.
const maxDepth = 64

func record(in stored) (search.Record, error) {
	data, err := json.Marshal(in)
	if err != nil {
		return search.Record{}, err
	}
	return search.Record{ID: in.LinkID, Data: data}, nil
}

// Indexed reports whether this machine holds a copy of the tree.
func (s *Service) Indexed() bool { return s.index != nil && s.index.Exists(search.AppDrive) }

// indexedTree is the account's own tree as the index holds it, brought up to
// date first, or nothing when there is no index to read.
//
// A tree somebody else shared, a computer's backup and a public link are not in
// it: the index is built from the volume this account's files live on, and
// answering for another one from it would answer about the wrong tree.
func (s *Service) indexedTree(ctx context.Context, dc *Context, prefix string) ([]Child, bool) {
	if !s.Indexed() || dc == nil || dc.Public() || dc.VolumeID == "" {
		return nil, false
	}
	status, err := s.index.Status(search.AppDrive)
	if err != nil || !status.Complete || status.Stale || status.Volume != dc.VolumeID {
		if err != nil {
			slog.DebugContext(ctx, "drive: the index could not be read, so the tree was walked",
				"kind", string(skip.KindVolume), "reason", string(skip.Unreadable), "error", err)
		}
		return nil, false
	}
	x, err := s.openIndex(ctx)
	if err != nil {
		slog.DebugContext(ctx, "drive: the index could not be opened, so the tree was walked",
			"kind", string(skip.KindVolume), "reason", string(skip.Unreadable), "error", err)
		return nil, false
	}
	x.dc = dc
	x.syncBeforeRead(ctx)
	// What the catch-up found out about the index counts as much as what the file
	// said before it: one that has just been told it owes a reading of the tree
	// answers for nothing.
	if !x.log.State.Complete || x.log.State.Stale {
		return nil, false
	}

	items := indexedItems(ctx, x.log)
	paths := make(map[string]string, len(items))
	under := strings.TrimRight(prefix, "/")
	out := make([]Child, 0, len(items))
	for linkID, in := range items {
		path, there := pathTo(items, paths, linkID, dc.RootLinkID, 0)
		if !there || !strings.HasPrefix(path, under+"/") {
			continue
		}
		out = append(out, in.child(path))
	}
	return out, true
}

// syncBeforeRead brings the index up to date before it is read, so an answer is
// never older than the command that asked for it. A directory another run is
// writing to is left alone: whatever holds it is keeping the index current.
func (x *indexSession) syncBeforeRead(ctx context.Context) {
	lock, err := x.s.index.Claim()
	if err != nil {
		slog.DebugContext(ctx, "drive: the index was busy, so it was read as it stands",
			"kind", string(skip.KindVolume), "reason", string(skip.Unreadable), "error", err)
		return
	}
	defer lock.Release()
	if _, err := x.Sync(ctx); err != nil {
		// Recorded and not counted: what the index holds is still every item the
		// last catch-up saw, and the tree it answers with says as much as it did
		// before this run started.
		slog.DebugContext(ctx, "drive: the index could not be brought up to date",
			"kind", string(skip.KindVolume), "reason", string(skip.Unreadable), "error", err)
	}
}
