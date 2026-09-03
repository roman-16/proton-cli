// Package runlog is the diagnostic log on disk: one file per invocation, in the
// order they happened, kept for as long as it is plausible somebody will report
// what went wrong in one of them.
//
// It exists because a bug cannot be reproduced on request. Somebody who hits an
// intermittent failure cannot be asked to hit it again with a flag set, and by
// the time they think to ask for help the run that failed is gone. So every run
// records what it did at full detail whether or not anything went wrong, and the
// price of that - a directory of small files - is paid up front and bounded.
//
// One file per day, holding every run that happened in it.
//
// The unit of retention has to be one a person can reason about, because the
// person deciding whether their failure is still here thinks "it happened on
// Tuesday" and never "it happened thirty-one runs ago". Counting runs also
// collapses under any burst: a script in a loop, or a test suite, can churn
// through a window measured in runs faster than somebody can notice the failure
// - a suite here was measured evicting thirty-two runs in forty-one seconds,
// which is a log that answers nothing about the run that failed.
//
// Every record carries the run it belongs to, so a day is read back as the runs
// that made it up. That is also what makes appending safe enough: each record is
// one write to a file opened for appending, and an interleaving that ever did
// tear one would cost that line rather than the file, which read already skips.
//
// What may be written into these files is not this package's business. It writes
// whatever the handler hands it; internal/redact decides what that is.
package runlog

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// DirName is the directory the log lives in, inside the CLI's own.
const DirName = "logs"

// Suffix is the extension. The files are JSON Lines: one record per line, so a
// file can be read with a text tool, parsed with a JSON one, and appended to
// without rewriting what is already there.
const Suffix = ".jsonl"

// keepFiles is how many days are kept. Counted rather than dated, so time away
// from the CLI does not erase what happened before it.
const keepFiles = 16

// Run is one invocation, and the day's file it appends to.
type Run struct {
	// ID names this run in every record it writes, which is what lets a day be
	// read back as the runs that made it up.
	ID   string
	Path string
	file *os.File
}

// Open starts a run's log in dir, creating the directory if it is not there.
//
// A failure to open is returned rather than swallowed so the caller can decide,
// and the caller decides the same thing every time: carry on without a log. A
// read-only home directory is not a reason to refuse to send mail.
func Open(dir string) (*Run, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, filename(time.Now()))
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	return &Run{ID: name(), Path: path, file: f}, nil
}

// Writer is where the handler writes. It is the open file and nothing wrapping
// it: a record has to reach the disk before the process does anything else that
// might be the thing that kills it.
func (r *Run) Writer() io.Writer { return r.file }

// Close finishes the file.
func (r *Run) Close() error {
	if r == nil || r.file == nil {
		return nil
	}
	return r.file.Close()
}

// filename is the day's file, named so that a directory listing is in date
// order and a person can see at a glance which days are here.
//
// The local date rather than UTC: somebody looking for what they did yesterday
// evening means their yesterday.
func filename(at time.Time) string { return at.Format("2006-01-02") + Suffix }

// name is a run's name: short enough to read in every line of a log, long
// enough that two runs in one session do not share one.
func name() string {
	b := make([]byte, 2)
	if _, err := rand.Read(b); err != nil {
		return "0000"
	}
	return hex.EncodeToString(b)
}

// Entry is a past run, read back off the disk.
type Entry struct {
	ID      string
	Path    string
	Started time.Time
	// Command is the command path the run was, without its arguments.
	Command string
	// Exit is the code the run ended with, and Ended says whether it got as far
	// as recording one - a run killed outright never does.
	Exit  int
	Ended bool
	// Lines are the records, as they were written.
	Lines []string
}

// Failed reports whether this run ended badly, which is what makes it the run a
// report is probably about.
func (e Entry) Failed() bool { return e.Ended && e.Exit != 0 }

// List reads every run still in dir, oldest first.
//
// A file that cannot be read, or a line in one that is not a record, is passed
// over rather than reported: this runs while somebody is trying to report a
// problem, and failing at it because one old log line is torn would be absurd.
func List(dir string) ([]Entry, error) {
	names, err := logFiles(dir)
	if err != nil {
		return nil, err
	}
	sort.Strings(names)
	var out []Entry
	for _, n := range names {
		out = append(out, read(filepath.Join(dir, n))...)
	}
	return out, nil
}

// Latest is the run a report is about: the most recent one that failed, or the
// most recent one at all when none did.
//
// The run doing the reporting is not a candidate. It is excluded by ID rather
// than by position, because "the last file" is whichever run happened to be
// slowest to finish and that is not a thing to guess about.
func Latest(dir, excluding string) (Entry, bool) {
	entries, err := List(dir)
	if err != nil {
		return Entry{}, false
	}
	var newest, newestFailed Entry
	var haveAny, haveFailed bool
	for _, e := range entries {
		if e.ID == excluding {
			continue
		}
		newest, haveAny = e, true
		if e.Failed() {
			newestFailed, haveFailed = e, true
		}
	}
	if haveFailed {
		return newestFailed, true
	}
	return newest, haveAny
}

// read parses one day into the runs that made it up, in the order they started.
//
// Records are grouped by the run they name rather than by where they sit, so
// two invocations that overlapped are read back as two runs and not as one
// interleaved mess. A line naming no run belongs to nothing and is passed over.
func read(path string) []Entry {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()

	var order []string
	runs := map[string]*Entry{}
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" {
			continue
		}
		var record struct {
			Time    time.Time `json:"time"`
			Run     string    `json:"run"`
			Command string    `json:"command"`
			Exit    *int      `json:"exit"`
		}
		if json.Unmarshal([]byte(line), &record) != nil || record.Run == "" {
			continue
		}
		e, seen := runs[record.Run]
		if !seen {
			e = &Entry{ID: record.Run, Path: path, Started: record.Time}
			runs[record.Run] = e
			order = append(order, record.Run)
		}
		e.Lines = append(e.Lines, line)
		if record.Command != "" {
			e.Command = record.Command
		}
		if record.Exit != nil {
			e.Exit, e.Ended = *record.Exit, true
		}
	}
	out := make([]Entry, 0, len(order))
	for _, id := range order {
		out = append(out, *runs[id])
	}
	return out
}

// Prune throws away the days nobody will report on, keeping the newest
// keepFiles of them.
//
// A day is named for its date and the dates sort as they read, so the newest are
// simply the last ones by name - which is also why today, the file this run is
// appending to, can never be what gets removed.
//
// It runs at the start of an invocation rather than at the end, so that a run
// killed before it could tidy up does not leave the directory growing for ever.
func Prune(dir string) {
	names, err := logFiles(dir)
	if err != nil {
		return
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	for _, n := range names[min(len(names), keepFiles):] {
		_ = os.Remove(filepath.Join(dir, n))
	}
}

// Clear removes every run. It is what uninstalling has to do: a directory of
// logs left behind by a binary that is gone is litter.
func Clear(dir string) error {
	err := os.RemoveAll(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func logFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), Suffix) {
			names = append(names, e.Name())
		}
	}
	return names, nil
}
