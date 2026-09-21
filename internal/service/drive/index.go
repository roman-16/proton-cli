package drive

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
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
// What it holds is the shape of the tree - names, sizes, times, and which folder
// each thing hangs in - and the text of the files that are text. Not where a
// thing sits, which is the folders' to say and is worked out when the tree is
// read: a folder that is renamed, moved or trashed takes everything under it
// with it, and the feed reports the folder alone. Not the keys that open any of
// it: a run that has to decrypt a name or a file fetches the folder's key again
// rather than keeping it.
//
// The text is a second pass, for the same reason a mailbox's bodies are. The
// tree comes back a folder at a time, so the whole of it is minutes and a
// filtered listing is answered from here as soon as it lands. A file's contents
// are a download each, so they follow, most recently changed first: the half of
// a drive somebody is going to search for is the half they have been working
// on, and a run stopped at any point has covered it.
//
// Which is why an item is counted when the index stops waiting on it - a folder
// or a file that is not text when its record lands, a text file when its
// contents do. Counting both halves would report a drive of eight thousand
// items as nine.

// stored is one link as the index holds it: what a listing shows, the parent it
// hangs from, and what the file says.
type stored struct {
	LinkID     string `json:"link_id"`
	ParentID   string `json:"parent_id,omitempty"`
	Name       string `json:"name"`
	Type       string `json:"type"`
	MIMEType   string `json:"mime_type,omitempty"`
	Size       int64  `json:"size,omitempty"`
	CreateTime int64  `json:"create_time,omitempty"`
	ModifyTime int64  `json:"modify_time,omitempty"`
	// Trashed is when the item was put in the trash. A trashed item is in the
	// account but not in the tree, so it is held and left out of a listing.
	Trashed int64 `json:"trashed,omitempty"`
	// Unreadable says the name would not decrypt with this account's keys, and
	// Sealed that this is a folder whose key would not open, so nothing inside it
	// was ever reached. Either way the item is held by what it does say about
	// itself.
	Unreadable bool `json:"unreadable,omitempty"`
	Sealed     bool `json:"sealed,omitempty"`
	// Revision is the version of the file the index last saw, and Text what that
	// version says. A file uploaded over has a new version, which is what makes
	// the text it held no longer the file's.
	Revision string `json:"revision,omitempty"`
	Text     string `json:"text,omitempty"`
	// Fetched says the version's contents were downloaded and decided on, and
	// Opaque that what came back is not text this can search - bytes that are not
	// text, or contents that would not open. One is the index holding what the
	// file says; the other is it having established that there is nothing to
	// hold, which is as settled as holding it.
	Fetched bool `json:"fetched,omitempty"`
	Opaque  bool `json:"opaque,omitempty"`
}

func (in stored) child(path string) Child {
	return Child{
		LinkID: in.LinkID, Name: displayName(in.Name, in.Unreadable), Path: path,
		Type: in.Type, Size: in.Size, CreateTime: in.CreateTime, ModifyTime: in.ModifyTime,
	}
}

// wantsText reports that the index should be holding this file's contents.
//
// The trash is left out with everything else about it: an item in it is in the
// account and out of the tree, so no listing would ever match its text.
func (in stored) wantsText() bool {
	return in.Type == TypeFile && in.Trashed == 0 &&
		in.Size > 0 && in.Size <= maxTextSize && isText(in.MIMEType, in.Name)
}

// settled reports that the index is not waiting on this file: it holds the
// text, it has established there is none to hold, or it never wanted any.
func (in stored) settled() bool { return in.Fetched || !in.wantsText() }

// whole reports that the index holds everything the account says about the
// item, which is what `index list` counts the exceptions to.
func (in stored) whole() bool { return !in.Unreadable && !in.Sealed && !in.Opaque }

// texted carries over what the index holds about a file's contents, where what
// Proton now says is the same version it read them from.
func (in stored) texted(before stored, had bool) stored {
	if !had || before.Revision != in.Revision || in.Revision == "" {
		return in
	}
	in.Text, in.Fetched, in.Opaque = before.Text, before.Fetched, before.Opaque
	return in
}

// indexed is one link as the index will hold it, from what Proton said about it.
func indexed(link *Link, name string, unreadable, sealed bool) stored {
	return stored{
		LinkID: link.LinkID, ParentID: link.ParentLinkID, Name: name,
		Type: linkType(link.Type), MIMEType: link.MIMEType, Size: link.Size,
		CreateTime: link.CreateTime, ModifyTime: link.ModifyTime, Trashed: link.Trashed,
		Unreadable: unreadable, Sealed: sealed, Revision: activeRevisionID(link),
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

// Count is how much a run would do: the tree, where one is owed a reading, and
// the texts it is still to download.
//
// There is no cheaper answer for the tree: how many things are in it is the
// thing the walk finds out, and Proton has nothing that says so. A preview of a
// first build therefore costs what the reading half of one costs, and writes
// nothing; a preview of the texts alone costs nothing at all.
func (p indexPart) Count(ctx context.Context) (int, error) {
	status, err := p.s.index.Status(search.AppDrive)
	if err != nil && !errors.Is(err, search.ErrNotIndexed) {
		return 0, err
	}
	owed := max(status.Texts-status.Bodies, 0)
	if status.Complete && !status.Stale {
		return owed, nil
	}
	dc, err := p.s.Resolve(ctx)
	if err != nil {
		return 0, err
	}
	count := 0
	_, err = p.s.walkFiles(ctx, dc, func(found) error {
		count++
		return nil
	})
	return count + owed, err
}

// indexSession is the Drive index, open: the log decrypted once, and what this
// run has worked out about what is in it.
type indexSession struct {
	s   *Service
	log *search.Log
	dc  *Context

	// surveyed says the records have been read through, which is what the counts
	// below are made of. It is put off until something needs them: a catch-up
	// that finds an empty feed - which is what a listing's is, nearly every time
	// - needs none of them, and reading them is the whole file.
	surveyed bool
	// texts is how many files the index holds the contents of, wanted how many
	// have contents to hold, and unreadable how many items went in without all of
	// what the account says about them.
	texts      int
	wanted     int
	unreadable int
	// owed is the files still to download, by link, with when each last changed -
	// which is the order they are fetched in.
	owed map[string]int64
}

func (s *Service) openIndex(ctx context.Context) (*indexSession, error) {
	log, err := s.index.Load(ctx, search.AppDrive)
	if err != nil {
		return nil, err
	}
	return &indexSession{s: s, log: log, owed: map[string]int64{}}, nil
}

func (x *indexSession) Status() search.Status {
	if x.surveyed {
		x.counts()
	}
	return x.log.Status()
}

// survey reads what is in the log, which is where the run's picture of it
// starts: how much of it is whole, and which files it owes contents to.
func (x *indexSession) survey(ctx context.Context) {
	if x.surveyed {
		return
	}
	x.surveyed = true
	for _, rec := range x.log.Records() {
		var in stored
		if err := json.Unmarshal(rec.Data, &in); err != nil {
			skip.Record(ctx, skip.KindItem, rec.ID, skip.Malformed, err)
			continue
		}
		x.reckon(stored{}, false, in)
	}
	x.counts()
}

// reckon takes one record into the run's picture of the index: an item may have
// gained the text it was owed, lost the text it held to a new version of the
// file, or stopped being a file whose text is held at all.
func (x *indexSession) reckon(before stored, had bool, now stored) {
	x.wanted += boolCount(now.wantsText()) - boolCount(had && before.wantsText())
	x.texts += boolCount(now.wantsText() && now.Fetched) - boolCount(had && before.wantsText() && before.Fetched)
	x.unreadable += boolCount(!now.whole()) - boolCount(had && !before.whole())
	if now.settled() {
		delete(x.owed, now.LinkID)
		return
	}
	x.owed[now.LinkID] = now.ModifyTime
}

// dropped takes an item the account no longer has out of the picture.
func (x *indexSession) dropped(before stored, had bool) {
	if !had {
		return
	}
	delete(x.owed, before.LinkID)
	x.wanted -= boolCount(before.wantsText())
	x.texts -= boolCount(before.wantsText() && before.Fetched)
	x.unreadable -= boolCount(!before.whole())
}

func boolCount(b bool) int {
	if b {
		return 1
	}
	return 0
}

// counts hands what the run has kept to the state beside the log, which is what
// `index list` reads and what a listing says its answer covers.
func (x *indexSession) counts() {
	x.log.State.Bodies, x.log.State.Texts = x.texts, x.wanted
	x.log.State.Unreadable = x.unreadable
}

// owing reports whether the summary beside the log says there are contents
// still to fetch.
//
// The summary is written after the work it summarises, so a run stopped between
// the two leaves one that says less than the log holds - never more. One that
// says nothing is owed can be taken at its word; one that says something is has
// to be checked against the log, which is what a survey does.
func (x *indexSession) owing() bool { return x.log.State.Bodies < x.log.State.Texts }

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

// save writes the state file, with the counts this run has kept, and with the
// log read through first where the summary claims there is work to do.
func (x *indexSession) save(ctx context.Context) error {
	if x.owing() {
		x.survey(ctx)
	}
	if x.surveyed {
		x.counts()
	}
	return x.log.Save(time.Now().Unix())
}

// Build reads the tree, and then the contents of the files it is owed.
//
// The walk runs when the index has never covered the volume, and when Proton
// has said it cannot describe what has happened to it - which is the only way
// to find out what changed while nothing was watching.
func (x *indexSession) Build(ctx context.Context, sink progress.Sink) (search.Result, error) {
	var done search.Result
	if !x.log.State.Complete || x.log.State.Stale {
		walked, err := x.walkVolume(ctx, sink)
		done = search.Total(done, walked)
		if err != nil {
			return done, err
		}
	}
	fetched, err := x.fetchOwedTexts(ctx, sink)
	return search.Total(done, fetched), err
}

// walkVolume walks the whole tree and writes down what it found.
//
// It is one pass rather than a resumable one: a walk is depth-first through
// folders whose keys are opened on the way, so there is no anchor to carry on
// from that is cheaper than starting again. What it costs is one request per
// folder, which is what a single filtered listing costs today.
//
// It runs again when Proton says it cannot describe what has happened to the
// volume. Then the walk is the answer: what came back is the tree, what did not
// is no longer in it, and neither is a thing the feed could have said.
func (x *indexSession) walkVolume(ctx context.Context, sink progress.Sink) (search.Result, error) {
	log := x.log
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
		if err := x.save(ctx); err != nil {
			return search.Result{}, err
		}
	}
	x.survey(ctx)

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
		in := indexed(&f.Link, f.Name, f.Unreadable, f.Sealed).texted(before, had)
		if had && before == in {
			return nil
		}
		rec, err := record(in)
		if err != nil {
			return err
		}
		records = append(records, rec)
		x.reckon(before, had, in)
		if in.settled() {
			done.Indexed++
		}
		if len(records) < indexBatch {
			return nil
		}
		if err := log.Append(records...); err != nil {
			return err
		}
		records = records[:0]
		return x.save(ctx)
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
	log.State.Total = log.State.Indexed
	log.State.Complete = tally.whole()
	log.State.Stale = !tally.whole()
	return done, x.save(ctx)
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
		before, had := held(x.log, rec.ID)
		records = append(records, search.Record{ID: rec.ID, Gone: true})
		x.dropped(before, had)
		done.Removed++
	}
	return done, x.log.Append(records...)
}

// fetchOwedTexts downloads the contents of every file the index holds without
// them, most recently changed first.
//
// An index that owes nothing is left unread. Working out what is owed means
// reading every record, and the commonest run there is - a catch-up on an index
// that is already current - would otherwise pay for the whole tree to find out
// that there is nothing to do.
func (x *indexSession) fetchOwedTexts(ctx context.Context, sink progress.Sink) (search.Result, error) {
	if x.owing() {
		x.survey(ctx)
	}
	wanted := x.owedNow()
	if len(wanted) == 0 {
		return search.Result{}, nil
	}
	dc, err := x.context(ctx)
	if err != nil {
		return search.Result{}, err
	}
	progress.Counting(sink, "texts")
	sink.Start(int64(x.wanted), "Indexing drive texts")
	progress.Resume(sink, int64(x.wanted-len(wanted)))

	names := newNaming(x.s, dc)
	var done search.Result
	for len(wanted) > 0 {
		batch := wanted[:min(textBatch, len(wanted))]
		wanted = wanted[len(batch):]
		got, err := x.fetchTexts(ctx, dc, names, batch, sink)
		done = search.Total(done, got)
		if err != nil {
			return done, err
		}
	}
	sink.Done()
	return done, x.save(ctx)
}

// owedNow is the files the index is still owed contents for, newest first, as
// the records to write once the text is there. A file stays owed until its text
// is written, so one whose download failed is asked for again by the next run,
// whether that is a new process or the next poll of the same one.
func (x *indexSession) owedNow() []stored {
	out := make([]stored, 0, len(x.owed))
	for id := range x.owed {
		in, had := held(x.log, id)
		if !had || in.settled() {
			delete(x.owed, id)
			continue
		}
		out = append(out, in)
	}
	slices.SortFunc(out, func(a, b stored) int {
		return cmp.Or(cmp.Compare(b.ModifyTime, a.ModifyTime), cmp.Compare(a.LinkID, b.LinkID))
	})
	return out
}

// fetchTexts downloads the contents of the files it is given and writes them
// down whole.
//
// What it is given is the record the index will hold, so one worker covers a
// build filling in what it walked and a catch-up taking in what just changed.
func (x *indexSession) fetchTexts(ctx context.Context, dc *Context, names *naming, want []stored, sink progress.Sink) (search.Result, error) {
	filled := make([]stored, len(want))
	var mu sync.Mutex
	var wg sync.WaitGroup
	slots := make(chan struct{}, textBatch)
	var failure error
	for i, in := range want {
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		slots <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-slots }()
			got, ok, err := x.fetchText(ctx, dc, names, in)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err != nil && failure == nil && ctx.Err() == nil:
				failure = err
			case err == nil && ok:
				filled[i] = got
			}
			if err == nil {
				sink.Add(1)
			}
		}()
	}
	wg.Wait()
	if failure != nil {
		return search.Result{}, failure
	}

	var done search.Result
	records := make([]search.Record, 0, len(filled))
	for _, in := range filled {
		if in.LinkID == "" {
			continue
		}
		rec, err := record(in)
		if err != nil {
			return done, err
		}
		records = append(records, rec)
		before, had := held(x.log, in.LinkID)
		x.reckon(before, had, in)
		if in.settled() {
			done.Indexed++
		}
		if in.Opaque {
			done.Unreadable++
		}
	}
	if err := x.log.Append(records...); err != nil {
		return done, err
	}
	// What was fetched before the run was stopped is kept: every record of it is
	// whole, and the next run is owed only what is still missing.
	return done, ctx.Err()
}

// fetchText is one file's contents, as the record that will hold them.
//
// Contents that will not open are not a reason to leave the file out. The
// alternative is a drive where a file exists in a listing and not in a search,
// which is the shape of a search nobody can trust; indexed without its text, it
// is still found by its name, its size and its date, and `index list` says how
// many are in that state.
//
// Contents that could not be asked for at all are a different thing, and are
// left owed rather than written off: the file keeps its place in the index and
// the next run asks again.
func (x *indexSession) fetchText(ctx context.Context, dc *Context, names *naming, in stored) (stored, bool, error) {
	link, err := x.s.getLink(ctx, dc.ShareID, in.LinkID)
	if err != nil {
		if ctx.Err() != nil {
			return stored{}, false, err
		}
		// Recorded and counted: the file stays in the index by its name, and what
		// is missing is the text a keyword would have matched.
		skip.Record(ctx, skip.KindItem, in.LinkID, skip.Unreadable, err)
		return stored{}, false, nil
	}
	in.MIMEType, in.Size, in.Revision = link.MIMEType, link.Size, activeRevisionID(link)
	if !in.wantsText() {
		// The file is not one whose contents are held any more - it grew, or it
		// went to the trash - and writing down what it is now settles it.
		return in, true, nil
	}
	parentKR, err := names.of(ctx, link.ParentLinkID)
	if err != nil {
		skip.Record(ctx, skip.KindItem, in.LinkID, skip.Unreadable, err)
		return stored{}, false, nil
	}
	nodeKR, err := unlockNode(link, parentKR, nil)
	if err != nil {
		// Recorded and counted where it shows: a key that will not open is this
		// account's permanent answer about the file, so the index settles for what
		// the listing already said about it and counts it as one it could not read.
		slog.DebugContext(ctx, "drive: a file's key would not open for the index",
			"kind", string(skip.KindItem), "reason", string(skip.Unlockable),
			"link", in.LinkID, "parent", link.ParentLinkID, "error", err)
		in.Text, in.Fetched, in.Opaque = "", true, true
		return in, true, nil
	}
	var buf bytes.Buffer
	if err := x.s.downloadFile(ctx, dc, link, nodeKR, in.Revision, link.Size, &buf, DownloadOptions{}); err != nil {
		if ctx.Err() != nil {
			return stored{}, false, err
		}
		skip.Record(ctx, skip.KindItem, in.LinkID, skip.Unreadable, err)
		return stored{}, false, nil
	}
	text, ok := asText(in.MIMEType, in.Name, buf.Bytes())
	in.Text, in.Fetched, in.Opaque = text, true, !ok
	return in, true, nil
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
// in is opened to read it - the one place following the tree costs keys. A file
// uploaded over arrives naming a version the index has no text for, which is
// what has the contents fetched again.
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

	names := newNaming(x.s, dc)
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
			return done, x.save(ctx)
		}
		applied, err := x.applyEvents(ctx, dc, names, batch)
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
	return done, x.save(ctx)
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
	log.State.Cursor, log.State.Volume = "", ""
	log.State.Stale = true
	done.Refreshed = true
	return done, x.save(ctx)
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
//
// An item whose name, place or size moved is rewritten from the event itself,
// which carries the whole link, and keeps whatever text the index holds for the
// version the file is still at. One that arrived or was uploaded over is owed
// its contents, and they are fetched here - so a download that fails leaves a
// file the index holds and will ask about again, rather than one it never heard
// of.
func (x *indexSession) applyEvents(ctx context.Context, dc *Context, names *naming, batch volumeEvents) (search.Result, error) {
	if len(batch.Events) == 0 {
		return search.Result{}, nil
	}
	x.survey(ctx)
	log := x.log
	var done search.Result
	var records []search.Record
	var fetch []stored
	var below *descendants
	for _, e := range batch.Events {
		before, had := held(log, e.Link.LinkID)
		if e.EventType == eventDelete {
			records = append(records, search.Record{ID: e.Link.LinkID, Gone: true})
			x.dropped(before, had)
			done.Removed++
			// Proton reports the item that was deleted and says nothing about what
			// was inside it, so a folder takes its contents out of the index here
			// or they stay in it for good.
			if before.Type == TypeFolder {
				if below == nil {
					below = childrenOf(ctx, log)
				}
				for _, id := range below.under(e.Link.LinkID) {
					gone, kept := held(log, id)
					records = append(records, search.Record{ID: id, Gone: true})
					x.dropped(gone, kept)
					done.Removed++
				}
			}
			continue
		}
		if e.EventType != eventCreate && e.EventType != eventUpdate && e.EventType != eventRename {
			continue
		}
		unreadable := false
		name, err := names.name(ctx, e.Link)
		if err != nil {
			// Recorded and counted where it shows: the item goes into the index by
			// everything but its name, which is what a walk would have shown as
			// well, and `index list` counts it under what it could not read.
			slog.DebugContext(ctx, "drive: a changed item's name could not be read",
				"kind", string(skip.KindFolder), "reason", string(skip.Unlockable),
				"link", e.Link.LinkID, "parent", e.Link.ParentLinkID, "error", err)
			unreadable = true
		}
		in := indexed(&e.Link, name, unreadable, had && before.Sealed).texted(before, had)
		if had && before == in {
			continue
		}
		rec, err := record(in)
		if err != nil {
			return done, err
		}
		records = append(records, rec)
		x.reckon(before, had, in)
		if in.settled() {
			done.Indexed++
			continue
		}
		fetch = append(fetch, in)
	}
	if err := log.Append(records...); err != nil {
		return done, err
	}
	if len(fetch) == 0 {
		return done, nil
	}
	fetched, err := x.fetchTexts(ctx, dc, names, fetch, progress.Nop{})
	return search.Total(done, fetched), err
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

// naming opens the folders a run has to read a name or a file out of, once
// each.
//
// Ten texts are fetched at a time and the files in one folder share its key, so
// the answer is kept behind a lock: what it costs is a request, and the workers
// that want the same folder wait for the first of them rather than each asking
// for it.
type naming struct {
	s    *Service
	dc   *Context
	mu   sync.Mutex
	keys map[string]*pgp.KeyRing
}

func newNaming(s *Service, dc *Context) *naming {
	return &naming{s: s, dc: dc, keys: map[string]*pgp.KeyRing{}}
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
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.open(ctx, linkID)
}

// open is of, with the folders already opened in hand. It recurses to itself
// rather than to of, which holds the lock it is called under.
func (f *naming) open(ctx context.Context, linkID string) (*pgp.KeyRing, error) {
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
	parentKR, err := f.open(ctx, link.ParentLinkID)
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
// date first, and what it did not cover where it cannot answer at all.
//
// A tree somebody else shared, a computer's backup and a public link are not in
// it: the index is built from the one share this account's own files hang from,
// and answering for another from it would answer about the wrong tree.
func (s *Service) indexedTree(ctx context.Context, dc *Context, prefix string, terms []string) ([]Child, Coverage, bool) {
	if dc == nil || dc.Public() || dc.Type != shareTypeMain || dc.VolumeID == "" {
		return nil, Coverage{Foreign: true}, false
	}
	if !s.Indexed() {
		return nil, Coverage{}, false
	}
	status, err := s.index.Status(search.AppDrive)
	if err != nil {
		slog.DebugContext(ctx, "drive: the index could not be read, so the tree was walked",
			"kind", string(skip.KindVolume), "reason", string(skip.Unreadable), "error", err)
		return nil, Coverage{Unfinished: true}, false
	}
	if !status.Complete || status.Stale || status.Volume != dc.VolumeID {
		return nil, Coverage{Unfinished: true}, false
	}
	x, err := s.openIndex(ctx)
	if err != nil {
		slog.DebugContext(ctx, "drive: the index could not be opened, so the tree was walked",
			"kind", string(skip.KindVolume), "reason", string(skip.Unreadable), "error", err)
		return nil, Coverage{Unfinished: true}, false
	}
	x.dc = dc
	x.syncBeforeRead(ctx)
	// What the catch-up found out about the index counts as much as what the file
	// said before it: one that has just been told it owes a reading of the tree
	// answers for nothing.
	if !x.log.State.Complete || x.log.State.Stale {
		return nil, Coverage{Unfinished: true}, false
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
		if !search.Matches(terms, in.Name, in.Text) {
			continue
		}
		out = append(out, in.child(path))
	}
	status = x.Status()
	return out, Coverage{Indexed: true, Texts: status.Texts, Read: status.Bodies}, true
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
