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
// What it holds is the shape of the tree: names, paths, sizes and times. Not the
// contents of a file, which are what Drive is mostly made of, and not the keys
// that open them - a run that has to decrypt a name fetches the folder's key
// again rather than keeping it.

// stored is one link as the index holds it: what a listing shows, and the
// parent it hangs from so a move can be applied without walking again.
type stored struct {
	LinkID     string `json:"link_id"`
	ParentID   string `json:"parent_id,omitempty"`
	Name       string `json:"name"`
	Path       string `json:"path"`
	Type       string `json:"type"`
	Size       int64  `json:"size,omitempty"`
	CreateTime int64  `json:"create_time,omitempty"`
	ModifyTime int64  `json:"modify_time,omitempty"`
	// Trashed is when the item was put in the trash. A trashed item is in the
	// account but not in the tree, so it is held and left out of a listing.
	Trashed int64 `json:"trashed,omitempty"`
}

func (in stored) child() Child {
	return Child{
		LinkID: in.LinkID, Name: in.Name, Path: in.Path, Type: in.Type,
		Size: in.Size, CreateTime: in.CreateTime, ModifyTime: in.ModifyTime,
	}
}

// SetIndex hands the service the profile's index directory.
func (s *Service) SetIndex(store *search.Store) { s.index = store }

// IndexPart is what `index` drives to keep the Drive index current.
func (s *Service) IndexPart() search.Part { return indexPart{s: s} }

type indexPart struct{ s *Service }

func (indexPart) App() search.App { return search.AppDrive }
func (indexPart) Noun() string    { return "items" }

func (p indexPart) Status() (search.Status, error) { return p.s.index.Status(search.AppDrive) }

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
	err = p.s.walkFiles(ctx, dc, func(Link, string, string) error {
		count++
		return nil
	})
	return count, err
}

// Build walks the whole tree once and writes it down.
//
// It is one pass rather than a resumable one: a walk is depth-first through
// folders whose keys are opened on the way, so there is no anchor to carry on
// from that is cheaper than starting again. What it costs is one request per
// folder, which is what a single filtered listing costs today.
func (p indexPart) Build(ctx context.Context, sink progress.Sink) (search.Result, error) {
	return p.s.buildIndex(ctx, progress.Of(sink))
}

// Sync applies what has happened to the volume since the last run.
func (p indexPart) Sync(ctx context.Context) (search.Result, error) {
	return p.s.syncIndex(ctx)
}

func (s *Service) buildIndex(ctx context.Context, sink progress.Sink) (search.Result, error) {
	log, err := s.index.Load(ctx, search.AppDrive)
	if err != nil {
		return search.Result{}, err
	}
	if log.State.Complete {
		return search.Result{}, nil
	}
	dc, err := s.Resolve(ctx)
	if err != nil {
		return search.Result{}, err
	}
	// The cursor is taken before the walk, so a file uploaded while it runs is
	// caught by the first sync rather than missed by both.
	if log.State.Cursor == "" {
		cursor, err := s.latestVolumeEvent(ctx, dc.VolumeID)
		if err != nil {
			return search.Result{}, err
		}
		log.State.Cursor = cursor
		log.State.Volume = dc.VolumeID
		if err := log.Save(time.Now().Unix()); err != nil {
			return search.Result{}, err
		}
	}

	// How many there are is what the walk is finding out, so the bar counts up
	// rather than towards anything and closes when the walk ends.
	sink.Start(0, "Indexing drive")
	var records []search.Record
	done := search.Result{}
	err = s.walkFiles(ctx, dc, func(l Link, name, path string) error {
		rec, err := record(stored{
			LinkID: l.LinkID, ParentID: l.ParentLinkID, Name: name, Path: path,
			Type: linkType(l.Type), Size: l.Size, CreateTime: l.CreateTime,
			ModifyTime: l.ModifyTime, Trashed: l.Trashed,
		})
		if err != nil {
			return err
		}
		records = append(records, rec)
		done.Indexed++
		sink.Add(1)
		if len(records) < indexBatch {
			return nil
		}
		if err := log.Append(records...); err != nil {
			return err
		}
		records = records[:0]
		return log.Save(time.Now().Unix())
	})
	if err != nil {
		return done, err
	}
	if err := log.Append(records...); err != nil {
		return done, err
	}
	sink.Done()
	log.State.Total = log.State.Indexed
	log.State.Complete = true
	return done, log.Save(time.Now().Unix())
}

// indexBatch is how many records one write carries. A build of a large tree
// would otherwise hold the whole of it before writing any of it, and lose the
// lot when somebody stops it.
const indexBatch = 200

// walkFiles walks the account's own file tree from its root.
func (s *Service) walkFiles(ctx context.Context, dc *Context, visit func(l Link, name, path string) error) error {
	res, err := s.ResolvePath(ctx, dc, "/")
	if err != nil {
		return err
	}
	return s.walkTree(ctx, dc, res.LinkID, res.NodeKR, "", visit)
}

// syncIndex applies the volume's change feed.
//
// Proton reports a link as it now is, so a file that was renamed, moved,
// trashed or restored arrives as the whole record and is written down again.
// What the event cannot carry is the name in the clear, so the folder it hangs
// in is opened to read it - the one place indexing Drive costs keys.
func (s *Service) syncIndex(ctx context.Context) (search.Result, error) {
	log, err := s.index.Load(ctx, search.AppDrive)
	if err != nil {
		return search.Result{}, err
	}
	if log.State.Cursor == "" {
		return search.Result{}, nil
	}
	dc, err := s.Resolve(ctx)
	if err != nil {
		return search.Result{}, err
	}
	// A volume that is not the one the index was built from is a different tree
	// altogether, so what is indexed says nothing about it.
	if log.State.Volume != "" && log.State.Volume != dc.VolumeID {
		log.State.Complete = false
		return search.Result{}, log.Save(time.Now().Unix())
	}

	names := &naming{s: s, dc: dc, keys: map[string]*pgp.KeyRing{}}
	var done search.Result
	for page := 0; page < maxDrain; page++ {
		var batch volumeEvents
		if err := s.C.Decode(ctx, proton.Request{
			Method: "GET",
			Path:   fmt.Sprintf("/drive/volumes/%s/events/%s", dc.VolumeID, log.State.Cursor),
		}, &batch); err != nil {
			return done, err
		}
		if batch.Refresh != 0 {
			slog.WarnContext(ctx, "The Drive index missed part of the volume's history and will be built again.",
				"kind", string(skip.KindVolume), "reason", string(skip.Unreadable))
			log.State.Complete = false
			cursor, err := s.latestVolumeEvent(ctx, dc.VolumeID)
			if err != nil {
				return done, err
			}
			log.State.Cursor = cursor
			return done, log.Save(time.Now().Unix())
		}
		log.State.Cursor = batch.EventID
		applied, err := s.applyEvents(ctx, log, names, batch)
		done = search.Total(done, applied)
		if err != nil {
			return done, err
		}
		if batch.More == 0 {
			break
		}
	}
	return done, log.Save(time.Now().Unix())
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
func (s *Service) applyEvents(ctx context.Context, log *search.Log, names *naming, batch volumeEvents) (search.Result, error) {
	var done search.Result
	var records []search.Record
	moved := false
	for _, e := range batch.Events {
		if e.EventType == eventDelete {
			records = append(records, search.Record{ID: e.Link.LinkID, Gone: true})
			done.Removed++
			continue
		}
		if e.EventType != eventCreate && e.EventType != eventUpdate && e.EventType != eventRename {
			continue
		}
		name, err := names.name(ctx, e.Link)
		if err != nil {
			// Recorded and counted: the item is in the account and not in the
			// index, so a listing drawn from the index is short by it and says so.
			skip.Record(ctx, skip.KindFolder, e.Link.ParentLinkID, skip.Unlockable, err)
			continue
		}
		before, had := held(log, e.Link.LinkID)
		rec, err := record(stored{
			LinkID: e.Link.LinkID, ParentID: e.Link.ParentLinkID, Name: name,
			Path: pathOf(log, e.Link.ParentLinkID, name), Type: linkType(e.Link.Type),
			Size: e.Link.Size, CreateTime: e.Link.CreateTime, ModifyTime: e.Link.ModifyTime,
			Trashed: e.Link.Trashed,
		})
		if err != nil {
			return done, err
		}
		records = append(records, rec)
		done.Indexed++
		// A folder that moved or was renamed takes every path under it with it,
		// which the feed does not report item by item.
		if had && e.Link.Type == protonFolder && (before.Name != name || before.ParentID != e.Link.ParentLinkID) {
			moved = true
		}
	}
	if err := log.Append(records...); err != nil {
		return done, err
	}
	if moved {
		return done, repath(ctx, log)
	}
	return done, nil
}

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

// pathOf is where an item sits, worked out from the folder it hangs in. An item
// whose parent is not indexed is left at its own name, which is what a tree
// built from an incomplete index can honestly say about it.
func pathOf(log *search.Log, parentID, name string) string {
	parent, ok := held(log, parentID)
	if !ok {
		return "/" + name
	}
	return parent.Path + "/" + name
}

// repath rewrites the paths under a folder that moved or was renamed.
//
// The feed reports the folder and says nothing about what is inside it, so the
// tree is put back together here: every item is given the path its parents now
// make, and the ones whose path changed are written down again.
func repath(ctx context.Context, log *search.Log) error {
	held := indexedItems(ctx, log)
	var rewritten []search.Record
	for id, in := range held {
		path := pathIn(held, in, 0)
		if path == in.Path {
			continue
		}
		in.Path = path
		rec, err := record(in)
		if err != nil {
			return err
		}
		rewritten = append(rewritten, rec)
		held[id] = in
	}
	return log.Append(rewritten...)
}

// pathIn is where an item sits according to the records around it. The depth is
// bounded so a parent that somehow names itself cannot spin.
func pathIn(held map[string]stored, in stored, depth int) string {
	parent, ok := held[in.ParentID]
	if !ok || depth > maxDepth {
		return "/" + in.Name
	}
	return pathIn(held, parent, depth+1) + "/" + in.Name
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
	if err != nil || !status.Complete || status.Volume != dc.VolumeID {
		if err != nil {
			slog.DebugContext(ctx, "drive: the index could not be read, so the tree was walked",
				"kind", string(skip.KindVolume), "reason", string(skip.Unreadable), "error", err)
		}
		return nil, false
	}
	s.syncBeforeRead(ctx)

	log, err := s.index.Load(ctx, search.AppDrive)
	if err != nil {
		slog.DebugContext(ctx, "drive: the index could not be opened, so the tree was walked",
			"kind", string(skip.KindVolume), "reason", string(skip.Unreadable), "error", err)
		return nil, false
	}
	under := strings.TrimRight(prefix, "/")
	out := make([]Child, 0, len(log.Records()))
	for _, in := range indexedItems(ctx, log) {
		if in.Trashed != 0 || !strings.HasPrefix(in.Path, under+"/") {
			continue
		}
		out = append(out, in.child())
	}
	return out, true
}

// syncBeforeRead brings the index up to date before it is read, so an answer is
// never older than the command that asked for it. A directory another run is
// writing to is left alone: whatever holds it is keeping the index current.
func (s *Service) syncBeforeRead(ctx context.Context) {
	lock, err := s.index.Claim()
	if err != nil {
		slog.DebugContext(ctx, "drive: the index was busy, so it was read as it stands",
			"kind", string(skip.KindVolume), "reason", string(skip.Unreadable), "error", err)
		return
	}
	defer lock.Release()
	if _, err := s.syncIndex(ctx); err != nil {
		// Recorded and not counted: what the index holds is still every item the
		// last catch-up saw, and the tree it answers with says as much as it did
		// before this run started.
		slog.DebugContext(ctx, "drive: the index could not be brought up to date",
			"kind", string(skip.KindVolume), "reason", string(skip.Unreadable), "error", err)
	}
}
