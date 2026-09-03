package idcache

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func newTestCache(t *testing.T) *Cache {
	t.Helper()
	return New(filepath.Join(t.TempDir(), "cache.json"))
}

func mail(refs ...string) []Entry {
	out := make([]Entry, 0, len(refs))
	for _, r := range refs {
		out = append(out, Entry{Collection: "mail messages", Ref: r})
	}
	return out
}

func refs(entries []Entry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Ref)
	}
	return out
}

func values(cands []Candidate) []string {
	out := make([]string, 0, len(cands))
	for _, c := range cands {
		out = append(out, c.Value)
	}
	return out
}

func TestSaveLoadRoundTrip(t *testing.T) {
	c := newTestCache(t)
	if err := c.Save(mail("a", "b", "c")...); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got := refs(c.load())
	want := []string{"a", "b", "c"}
	if !equalSlice(got, want) {
		t.Errorf("load: got %v, want %v", got, want)
	}
}

func TestSaveKeepsOneEntryPerThing(t *testing.T) {
	c := newTestCache(t)
	if err := c.Save(mail("a", "b")...); err != nil {
		t.Fatal(err)
	}
	if err := c.Save(mail("b", "c", "a")...); err != nil {
		t.Fatal(err)
	}
	got := refs(c.load())
	want := []string{"a", "b", "c"}
	if !equalSlice(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// The same reference in two collections is two things: an attachment listing
// shows the message it came from, and neither entry may swallow the other.
func TestSaveKeepsTheSameRefInTwoCollections(t *testing.T) {
	c := newTestCache(t)
	if err := c.Save(
		Entry{Collection: "mail messages", Ref: "a"},
		Entry{Collection: "mail conversations", Ref: "a"},
	); err != nil {
		t.Fatal(err)
	}
	if got := len(c.load()); got != 2 {
		t.Errorf("got %d entries, want 2", got)
	}
}

// Seeing something again is how a handle arrives: the listing that showed only
// an ID leaves one entry, and the listing that showed a name fills it in.
func TestSavePrefersTheNewestReading(t *testing.T) {
	c := newTestCache(t)
	_ = c.Save(Entry{Collection: "mail messages", Ref: "a"})
	_ = c.Save(Entry{Collection: "mail messages", Ref: "a", Handles: []string{"Invoice"}})
	got := c.load()
	if len(got) != 1 || len(got[0].Handles) != 1 || got[0].Handles[0] != "Invoice" {
		t.Errorf("got %+v, want one entry handled Invoice", got)
	}
}

func TestSaveDropsWhatNamesNothing(t *testing.T) {
	c := newTestCache(t)
	if err := c.Save(
		Entry{Collection: "mail messages", Ref: ""},
		Entry{Collection: "mail messages", Ref: "a"},
		Entry{Collection: "", Ref: "b"},
	); err != nil {
		t.Fatal(err)
	}
	if got := refs(c.load()); !equalSlice(got, []string{"a"}) {
		t.Errorf("entries with nothing to file were kept: %v", got)
	}
}

func TestSavePrunesAtMaxEntries(t *testing.T) {
	c := newTestCache(t)
	seed := make([]Entry, MaxEntries)
	for i := range seed {
		seed[i] = Entry{Collection: "mail messages", Ref: fmt.Sprintf("seed-%05d", i)}
	}
	if err := c.Save(seed...); err != nil {
		t.Fatal(err)
	}
	if got := len(c.load()); got != MaxEntries {
		t.Fatalf("expected %d entries after seed, got %d", MaxEntries, got)
	}
	if err := c.Save(mail("new-entry")...); err != nil {
		t.Fatal(err)
	}
	loaded := c.load()
	if got := len(loaded); got != MaxEntries {
		t.Errorf("expected %d entries after prune, got %d", MaxEntries, got)
	}
	if loaded[0].Ref == "seed-00000" {
		t.Errorf("oldest entry should have been pruned; head=%q", loaded[0].Ref)
	}
	if loaded[len(loaded)-1].Ref != "new-entry" {
		t.Errorf("newest entry not at tail; tail=%q", loaded[len(loaded)-1].Ref)
	}
}

func TestResolveExactMatch(t *testing.T) {
	c := newTestCache(t)
	_ = c.Save(mail("abc12345xyz", "def67890qwerty")...)
	got, err := c.Resolve("abc12345")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != "abc12345xyz" {
		t.Errorf("got %q, want abc12345xyz", got)
	}
}

// A compound reference is stored as the one token that was printed, and either
// half of it answers to its own short form.
func TestResolveFindsEitherHalfOfACompoundReference(t *testing.T) {
	c := newTestCache(t)
	_ = c.Save(Entry{Collection: "calendar events", Ref: "cal12345xyz/evt67890abc"})
	for short, want := range map[string]string{
		"cal12345": "cal12345xyz",
		"evt67890": "evt67890abc",
	} {
		got, err := c.Resolve(short)
		if err != nil {
			t.Fatalf("Resolve(%q): %v", short, err)
		}
		if got != want {
			t.Errorf("Resolve(%q) = %q, want %q", short, got, want)
		}
	}
}

// Every event in one calendar carries that calendar's ID, so the same half is
// seen over and over. That is one thing, not a hundred candidates.
func TestResolveDoesNotCallARepeatedHalfAmbiguous(t *testing.T) {
	c := newTestCache(t)
	_ = c.Save(
		Entry{Collection: "calendar events", Ref: "cal12345xyz/evt00000one"},
		Entry{Collection: "calendar events", Ref: "cal12345xyz/evt11111two"},
	)
	got, err := c.Resolve("cal12345")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != "cal12345xyz" {
		t.Errorf("got %q, want cal12345xyz", got)
	}
}

func TestResolveNotFound(t *testing.T) {
	c := newTestCache(t)
	_ = c.Save(mail("abc12345xyz")...)
	_, err := c.Resolve("nope")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestResolveAmbiguous(t *testing.T) {
	c := newTestCache(t)
	_ = c.Save(mail("abc12345first", "abc12345second", "def67890other")...)
	_, err := c.Resolve("abc12345")
	var amb *AmbiguousError
	if !errors.As(err, &amb) {
		t.Fatalf("expected *AmbiguousError, got %v", err)
	}
	if amb.Prefix != "abc12345" {
		t.Errorf("Prefix = %q, want abc12345", amb.Prefix)
	}
	sort.Strings(amb.Candidates)
	want := []string{"abc12345first", "abc12345second"}
	if !equalSlice(amb.Candidates, want) {
		t.Errorf("Candidates = %v, want %v", amb.Candidates, want)
	}
}

func TestCandidatesOfferTheShortFormAndEveryHandle(t *testing.T) {
	c := newTestCache(t)
	_ = c.Save(Entry{
		Collection: "contacts", Ref: "QmxLp2RtAAAAAAAA",
		Handles: []string{"Jane Doe", "jane@example.com"},
	})
	got := values(c.Candidates("contacts", ""))
	want := []string{"QmxLp2Rt", "Jane Doe", "jane@example.com"}
	if !equalSlice(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestCandidatesAreFilteredByWhatWasTyped(t *testing.T) {
	c := newTestCache(t)
	_ = c.Save(Entry{
		Collection: "contacts", Ref: "QmxLp2RtAAAAAAAA",
		Handles: []string{"Jane Doe", "jane@example.com"},
	})
	for typed, want := range map[string][]string{
		"Qmx":  {"QmxLp2Rt"},
		"jane": {"Jane Doe", "jane@example.com"},
		"zz":   {},
	} {
		if got := values(c.Candidates("contacts", typed)); !equalSlice(got, want) {
			t.Errorf("Candidates(%q) = %v, want %v", typed, got, want)
		}
	}
}

// Eight characters is what a listing prints, but a full ID pasted from a script
// is longer than that, and the completion has to grow rather than shrink it.
func TestCandidatesOfferTheWholeRefOnceMoreThanEightIsTyped(t *testing.T) {
	c := newTestCache(t)
	_ = c.Save(mail("abc12345xyzlonger")...)
	if got := values(c.Candidates("mail messages", "abc12345x")); !equalSlice(got, []string{"abc12345xyzlonger"}) {
		t.Errorf("got %v, want the whole reference", got)
	}
}

func TestCandidatesSeeOnlyTheirOwnCollection(t *testing.T) {
	c := newTestCache(t)
	_ = c.Save(
		Entry{Collection: "mail messages", Ref: "aaaa1111bbbb"},
		Entry{Collection: "pass items", Ref: "aaaa2222cccc"},
	)
	if got := values(c.Candidates("pass items", "aaaa")); !equalSlice(got, []string{"aaaa2222"}) {
		t.Errorf("got %v, want only the Pass item", got)
	}
}

// The listing just run is the one being typed against, so it comes first.
func TestCandidatesAreNewestFirst(t *testing.T) {
	c := newTestCache(t)
	_ = c.Save(mail("aaaa1111older")...)
	_ = c.Save(mail("aaaa2222newer")...)
	if got := values(c.Candidates("mail messages", "aaaa")); !equalSlice(got, []string{"aaaa2222", "aaaa1111"}) {
		t.Errorf("got %v, want the newest first", got)
	}
}

func TestLoadMissingFileIsEmpty(t *testing.T) {
	c := newTestCache(t)
	if got := c.load(); len(got) != 0 {
		t.Errorf("expected empty slice on missing file, got %v", got)
	}
}

func TestLoadUnreadableFileIsEmpty(t *testing.T) {
	c := newTestCache(t)
	if err := os.MkdirAll(filepath.Dir(c.path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(c.path, []byte("not json"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := c.load(); len(got) != 0 {
		t.Errorf("expected empty slice on unreadable file, got %v", got)
	}
	if err := c.Save(mail("a")...); err != nil {
		t.Errorf("Save on unreadable file failed: %v", err)
	}
	if got := refs(c.load()); !equalSlice(got, []string{"a"}) {
		t.Errorf("Save did not overwrite an unreadable file: %v", got)
	}
}

func TestSaveAtomicWrite(t *testing.T) {
	// The on-disk format is a JSON array of entries, so an external tool can
	// read it directly.
	c := newTestCache(t)
	_ = c.Save(Entry{Collection: "mail messages", Ref: "a", Handles: []string{"Invoice"}})
	data, err := os.ReadFile(c.path)
	if err != nil {
		t.Fatal(err)
	}
	var got []Entry
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("on-disk format is not a JSON array: %v\n%s", err, string(data))
	}
	if len(got) != 1 || got[0].Collection != "mail messages" || got[0].Ref != "a" {
		t.Errorf("got %+v, want the entry that was saved", got)
	}
}

func TestClear(t *testing.T) {
	c := newTestCache(t)
	_ = c.Save(mail("a")...)
	if err := c.Clear(); err != nil {
		t.Fatal(err)
	}
	if got := c.load(); len(got) != 0 {
		t.Errorf("expected empty after Clear, got %v", got)
	}
	// Clearing a missing file is fine.
	if err := c.Clear(); err != nil {
		t.Errorf("Clear on missing file errored: %v", err)
	}
}

func equalSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
