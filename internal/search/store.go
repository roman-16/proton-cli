package search

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/roman-16/proton-cli/internal/crypto/aead"
)

// A log is a sequence of sealed records, each framed by its own length.
//
// Appending is the whole of how one is written: a build that stops halfway has
// written every record it got to, and what it did not get to is what the next
// run works out from what is there. A record that supersedes an earlier one is
// appended too, which is what makes a change one write rather than a rewrite of
// the file - and what makes compaction a thing that happens later, once the
// superseded records outweigh what is live.
//
// Nothing outside the sealed part of a record says anything: two records of the
// same thing are indistinguishable from two records of different things, and the
// only thing the frame gives away is how long each is.
const (
	// frameLen is the length prefix in front of every sealed record.
	frameLen = 4
	// maxRecord is the largest record that will be read back, which is what
	// stops a corrupt length prefix from asking for a gigabyte.
	maxRecord = 64 << 20
	// compactAtWaste is how much of a log may be superseded records before it is
	// worth rewriting. Half is a file twice the size it needs to be, and every
	// rewrite costs a full read and write of the whole thing.
	compactAtWaste = 0.5
)

// Record is one thing in a log.
type Record struct {
	// ID is the thing this record is about. Records of the same ID supersede one
	// another, newest last.
	ID string `json:"id,omitempty"`
	// Gone says the thing is no longer in the account, which a search has to
	// know and a log cannot express by leaving something out.
	Gone bool `json:"gone,omitempty"`
	// Data is the app's own shape for the thing.
	Data json.RawMessage `json:"data,omitempty"`
}

// State is what an index knows about itself, and the one part of it that is not
// sealed: counts, timestamps and the cursor into Proton's change feed. It names
// nothing in the account.
type State struct {
	// Account is the user the index was built for, so an index left behind by
	// another one is refused rather than decrypted into noise.
	Account string `json:"account,omitempty"`
	// Cursor is where the app's change feed had got to when the index was last
	// brought up to date, and Volume the container that feed belongs to, for an
	// app whose feed is per container rather than per account.
	Cursor string `json:"cursor,omitempty"`
	Volume string `json:"volume,omitempty"`
	// Cursors is the same, for an app whose feed is per container: one calendar
	// has a history of its own, and catching up with one says nothing about the
	// others.
	Cursors map[string]string `json:"cursors"`
	// Indexed and Total are how much is in the log and how much there was to
	// index. Unreadable is how much went in without its content.
	Indexed    int `json:"indexed"`
	Total      int `json:"total"`
	Unreadable int `json:"unreadable,omitempty"`
	// Bodies is how many of the things in the log hold the text that has to be
	// fetched one at a time, which is what a mailbox is indexed in two passes
	// for: an envelope is a row of a listing, a body is a request of its own.
	Bodies int `json:"bodies,omitempty"`
	// Complete says the first build reached the end.
	Complete bool `json:"complete"`
	// Stale says what is in the index can no longer be trusted to be what the
	// account holds: Proton could not describe what has happened since it was
	// last brought up to date, or a record of it would not read back. It is
	// settled by reading the account again rather than by following the feed,
	// which is why it is the build that clears it.
	Stale bool `json:"stale,omitempty"`
	// Oldest is the far end of what is indexed, which is what a search over a
	// half-built index has to say about what it did not cover.
	Oldest  int64 `json:"oldest,omitempty"`
	Updated int64 `json:"updated"`
}

// Log is one app's index, open.
type Log struct {
	store *Store
	app   App
	key   []byte

	// State is the index's own account of itself, saved by Save.
	State State

	recs []Record
	at   map[string]int
	// parsed is how far the file could be read frame by frame, so a torn tail is
	// written over rather than left in front of everything appended after it.
	parsed int64
	// dead is how many records in the file are superseded by a later one.
	dead int
	// lost is how many bytes at the end of the file did not read back, which is
	// what an interrupted write leaves.
	lost int64
	// corrupt is how many frames were whole and would not open, which no write
	// of this program leaves and which mend rewrites the file without.
	corrupt int
}

// Load opens an app's index for reading and appending, creating the key and the
// directory if this is the first thing to ask for them.
func (s *Store) Load(ctx context.Context, app App) (*Log, error) {
	st, err := s.state(app)
	if err != nil && !errors.Is(err, ErrNotIndexed) {
		return nil, err
	}
	key, err := s.indexKey(ctx)
	if err != nil {
		return nil, err
	}
	l := &Log{store: s, app: app, key: key, State: st, at: map[string]int{}}
	if err := l.read(); err != nil {
		return nil, err
	}
	l.note(ctx)

	if l.State.Account == "" {
		l.State.Account = s.account()
	}
	// The log is what is there; the count beside it is a summary that a run
	// stopped between the two would have left behind.
	l.State.Indexed = len(l.recs)
	return l, nil
}

// Records is what the index holds, in the order it was written.
func (l *Log) Records() []Record { return l.recs }

// Status is what the open index holds, as a listing of indexes reports it.
func (l *Log) Status() Status { return statusOf(l.app, l.State, l.store.bytes(l.app)) }

// Has reports whether the index already holds something.
func (l *Log) Has(id string) bool { _, ok := l.at[id]; return ok }

// Get is what the index holds about one thing.
func (l *Log) Get(id string) (Record, bool) {
	i, ok := l.at[id]
	if !ok {
		return Record{}, false
	}
	return l.recs[i], true
}

// note says what did not read back, for the log a report is made from.
func (l *Log) note(ctx context.Context) {
	if l.lost > 0 {
		// Recorded and not counted: the records are written again by the run that
		// carries on, and what is in the index is what `index list` reports.
		slog.DebugContext(ctx, "search: the end of the index did not read back", "bytes", l.lost)
	}
	if l.corrupt > 0 {
		slog.WarnContext(ctx, fmt.Sprintf("%d of the %s index's records would not open, so it will be read from Proton again.", l.corrupt, l.app),
			"count", l.corrupt)
	}
}

// Append seals records onto the end of the log and remembers them.
//
// One write carries the whole batch, so a page of a build lands or does not: a
// reader that arrives mid-write sees the records before it and stops at the
// first frame that is not whole yet.
func (l *Log) Append(recs ...Record) error {
	if len(recs) == 0 {
		return nil
	}
	if err := l.mend(); err != nil {
		return err
	}
	buf, err := l.frames(recs)
	if err != nil {
		return err
	}
	if err := l.store.ensureDir(); err != nil {
		return err
	}
	f, err := os.OpenFile(l.store.logPath(l.app), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	// A torn tail from an interrupted write is cut before anything is added, so
	// the records after it are not stranded behind a frame nothing can read.
	if size, err := f.Seek(0, 2); err == nil && size > l.parsed {
		if err := f.Truncate(l.parsed); err != nil {
			return err
		}
	}
	if _, err := f.Write(buf); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	l.parsed += int64(len(buf))
	for _, r := range recs {
		l.remember(r)
	}
	l.State.Indexed = len(l.recs)
	if l.worthCompacting() {
		return l.compact()
	}
	return nil
}

// Save writes the state file. The log is the index; this is the summary beside
// it, and it is rewritten whole because it is four lines long.
func (l *Log) Save(now int64) error {
	if err := l.mend(); err != nil {
		return err
	}
	l.State.Updated = now
	l.State.Indexed = len(l.recs)
	if err := l.store.ensureDir(); err != nil {
		return err
	}
	data, err := json.Marshal(l.State)
	if err != nil {
		return err
	}
	return writeFile(l.store.statePath(l.app), data)
}

// remember files a record where the next read of the same thing will find it.
func (l *Log) remember(r Record) {
	if r.ID == "" {
		return
	}
	if i, ok := l.at[r.ID]; ok {
		l.dead++
		if r.Gone {
			l.drop(i)
			return
		}
		l.recs[i] = r
		return
	}
	if r.Gone {
		// A thing that was never indexed and is now gone leaves nothing behind;
		// the record is what says so to a reader catching up from further back.
		l.dead++
		return
	}
	l.at[r.ID] = len(l.recs)
	l.recs = append(l.recs, r)
}

// drop removes a record the account no longer has.
func (l *Log) drop(i int) {
	delete(l.at, l.recs[i].ID)
	l.recs = append(l.recs[:i], l.recs[i+1:]...)
	for id, j := range l.at {
		if j > i {
			l.at[id] = j - 1
		}
	}
}

// read loads the log, telling a tail an interrupted write left from a record
// that went bad where it lay.
//
// A frame that reaches past the end of the file, or a length nothing could have
// written in front of nothing but zeros, is a run interrupted between a write
// and its end, which is the ordinary way a build stops: the records in front of
// it are every bit as good as they were, and the next append writes over it.
//
// A frame that is whole and will not open is something else. What it held is
// gone, and nothing in the file says what it was, so the index is marked as
// owing a reading of the account; the frames after it are read on rather than
// thrown away with it, and the file is rewritten without it under the first
// write.
func (l *Log) read() error {
	data, err := os.ReadFile(l.store.logPath(l.app))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for at := 0; at+frameLen <= len(data); {
		size := int(binary.BigEndian.Uint32(data[at : at+frameLen]))
		end := at + frameLen + size
		if size <= 0 || size > maxRecord {
			if !zeros(data[at:]) {
				l.corrupt++
			}
			break
		}
		if end > len(data) {
			break
		}
		r, err := l.open(data[at+frameLen : end])
		at = end
		l.parsed = int64(at)
		if err != nil {
			l.corrupt++
			continue
		}
		l.remember(r)
	}
	l.lost = int64(len(data)) - l.parsed
	if l.corrupt > 0 {
		l.State.Stale = true
	}
	return nil
}

// zeros reports whether a tail holds nothing, which is what a crash leaves in a
// file it had extended and not yet written to.
func zeros(tail []byte) bool { return len(bytes.Trim(tail, "\x00")) == 0 }

// frames seals records and puts a length in front of each, as the file holds
// them.
func (l *Log) frames(recs []Record) ([]byte, error) {
	var buf []byte
	for _, r := range recs {
		sealed, err := l.seal(r)
		if err != nil {
			return nil, err
		}
		frame := make([]byte, frameLen)
		binary.BigEndian.PutUint32(frame, uint32(len(sealed)))
		buf = append(append(buf, frame...), sealed...)
	}
	return buf, nil
}

func (l *Log) seal(r Record) ([]byte, error) {
	plain, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}
	return aead.Encrypt(l.key, plain, l.store.aad(l.app))
}

func (l *Log) open(sealed []byte) (Record, error) {
	plain, err := aead.Decrypt(l.key, sealed, l.store.aad(l.app))
	if err != nil {
		return Record{}, err
	}
	var r Record
	if err := json.Unmarshal(plain, &r); err != nil {
		return Record{}, err
	}
	return r, nil
}

// worthCompacting reports whether the file is mostly records nothing reads.
func (l *Log) worthCompacting() bool {
	total := len(l.recs) + l.dead
	return total > 64 && float64(l.dead) > float64(total)*compactAtWaste
}

// mend rewrites a log that was found holding a frame nothing can open. It is
// done under the run's first write rather than at the reading, because a
// reading may be a search's while another run holds the directory.
func (l *Log) mend() error {
	if l.corrupt == 0 {
		return nil
	}
	return l.compact()
}

// compact rewrites the log as the records it currently holds, under fresh
// nonces, and swaps it in. A crash partway leaves the old file in place.
func (l *Log) compact() error {
	buf, err := l.frames(l.recs)
	if err != nil {
		return err
	}
	if err := writeFile(l.store.logPath(l.app), buf); err != nil {
		return err
	}
	l.parsed, l.dead, l.lost, l.corrupt = int64(len(buf)), 0, 0, 0
	return nil
}

// state reads an app's state file. An app with none has no index.
func (s *Store) state(app App) (State, error) {
	data, err := os.ReadFile(s.statePath(app))
	if errors.Is(err, os.ErrNotExist) {
		return State{}, ErrNotIndexed
	}
	if err != nil {
		return State{}, err
	}
	var st State
	if err := json.Unmarshal(data, &st); err != nil {
		return State{}, fmt.Errorf("search: read %s: %w", s.statePath(app), err)
	}
	if err := s.mine(st); err != nil {
		return State{}, err
	}
	return st, nil
}

// writeFile writes a file the way everything else this CLI keeps per profile is
// written: to a temporary neighbour, then renamed over the target, so a crash
// leaves the previous one whole.
func writeFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer func() { _ = os.Remove(tmp) }()
	if err := f.Chmod(0600); err != nil {
		_ = f.Close()
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
