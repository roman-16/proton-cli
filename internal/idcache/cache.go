// Package idcache stores the references the user has recently been shown, so
// that a short ID read off a listing resolves in the next command and so that a
// shell can offer back what was on the screen. What is stored is always the
// reference exactly as it was printed, together with the collection it belongs
// to and the names a person would use for it; internal/ref owns both how a
// reference is shortened and how the short form is matched against it.
//
// The cache is a per-profile JSON array at
// ~/.config/proton-cli/idcache/<profile>.json. Writes are atomic
// (write to tmp, rename) and best-effort: a missing or unreadable cache
// reads as empty without surfacing an error to the caller.
package idcache

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/roman-16/proton-cli/internal/ref"
)

// MaxEntries caps the cache size. Older entries are pruned FIFO on write.
const MaxEntries = 10000

// ErrNotFound is returned by Resolve when no cached reference has the given
// prefix.
var ErrNotFound = errors.New("idcache: prefix not found")

// AmbiguousError is returned by Resolve when two or more cached references
// share the same prefix.
type AmbiguousError struct {
	Prefix     string
	Candidates []string
}

func (e *AmbiguousError) Error() string {
	return fmt.Sprintf("idcache: %d IDs match prefix %q", len(e.Candidates), e.Prefix)
}

// Entry is one reference a response showed.
type Entry struct {
	// Collection is the command line that lists this kind of thing, such as
	// "mail conversations". It is what keeps a completion for a message from
	// offering a vault.
	Collection string `json:"collection"`
	// Ref is the reference as it was printed, which is the token the user can
	// paste back - a single ID, or the two an event or a Pass item is addressed
	// by, joined.
	Ref string `json:"ref"`
	// Handles are the names a person would use for it: a subject, a title, a
	// display name, an address.
	Handles []string `json:"handles"`
}

// key identifies the thing an entry is about, so that seeing it again updates
// what is known about it rather than storing it twice.
func (e Entry) key() string { return e.Collection + "\x00" + e.Ref }

// Candidate is one thing a shell may offer back, and what to show beside it.
type Candidate struct {
	// Value is what would be typed onto the command line.
	Value string
	// About is the other half of what was on the screen: the handle for a
	// reference, and empty for a handle, which needs no gloss of itself.
	About string
}

// Cache is the per-profile cache of what has been shown. The zero value is
// unsafe; use New.
type Cache struct {
	path string
	mu   sync.Mutex

	entries []Entry
	loaded  bool
}

// New constructs a Cache backed by the given file path. Parent directories
// are created on first write.
func New(path string) *Cache { return &Cache{path: path} }

func (c *Cache) Path() string { return c.path }

// Save merges entries into the cache, keeps the newest reading of each thing,
// FIFO-prunes to MaxEntries, and atomically rewrites the file.
//
// Empty calls are a no-op. Failures are returned but ignored by callers in the
// response path, so a cache hiccup never fails a user-visible command.
func (c *Cache) Save(entries ...Entry) error {
	if len(entries) == 0 {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	out := make([]Entry, 0, len(c.load())+len(entries))
	at := make(map[string]int, cap(out))
	for _, e := range append(c.load(), entries...) {
		if e.Ref == "" || e.Collection == "" {
			continue
		}
		if i, ok := at[e.key()]; ok {
			out[i] = e
			continue
		}
		at[e.key()] = len(out)
		out = append(out, e)
	}
	if len(out) > MaxEntries {
		out = out[len(out)-MaxEntries:]
	}
	c.entries = out
	return c.writeAtomic(out)
}

// Resolve returns the full ID a short one names. Returns ErrNotFound on miss,
// *AmbiguousError when several cached IDs answer to the same short form.
//
// It looks across every collection, because a short ID is read off a screen
// rather than out of a command line: the person pasting one back is not saying
// what kind of thing it was, and a resolution that depended on which command
// was asking would fail on the compound references, whose halves belong to two
// collections at once.
func (c *Cache) Resolve(prefix string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	var hits []string
	seen := make(map[string]struct{})
	for _, e := range c.load() {
		parts, _ := ref.Split(e.Ref)
		for _, id := range parts {
			if _, ok := seen[id]; ok || !ref.Matches(id, prefix) {
				continue
			}
			seen[id] = struct{}{}
			hits = append(hits, id)
		}
	}
	switch len(hits) {
	case 0:
		return "", ErrNotFound
	case 1:
		return hits[0], nil
	default:
		return "", &AmbiguousError{Prefix: prefix, Candidates: hits}
	}
}

// Candidates returns what the collection has shown that begins with prefix,
// newest first.
//
// A reference answers to its short form and to its whole self, so that eight
// characters read off a listing and a full ID pasted from a script both grow
// into something the command accepts. A handle matches without regard to case,
// because that is how a person types a subject back.
func (c *Cache) Candidates(collection, prefix string) []Candidate {
	c.mu.Lock()
	defer c.mu.Unlock()

	var out []Candidate
	seen := make(map[string]struct{})
	add := func(value, about string) {
		if value == "" || !strings.HasPrefix(strings.ToLower(value), strings.ToLower(prefix)) {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		out = append(out, Candidate{Value: value, About: about})
	}
	entries := c.load()
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		if e.Collection != collection {
			continue
		}
		var about string
		if len(e.Handles) > 0 {
			about = e.Handles[0]
		}
		if short := ref.Shorten(e.Ref); len(prefix) > len(short) {
			add(e.Ref, about)
		} else {
			add(short, about)
		}
		for _, h := range e.Handles {
			add(h, "")
		}
	}
	return out
}

// Clear removes the cache file.
func (c *Cache) Clear() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries, c.loaded = nil, true
	if err := os.Remove(c.path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// load reads the cache from disk, once. A run resolves a reference per argument
// and a bulk command carries dozens, so re-reading the file each time would cost
// the same parse over and over for an answer that cannot have changed: this
// process is the only thing writing to it while it runs.
//
// A file that is missing, or that holds anything but this, reads as empty and no
// error: what it holds can be shown again by listing again, so discarding it
// costs a listing and reporting it would cost a command.
func (c *Cache) load() []Entry {
	if c.loaded {
		return c.entries
	}
	c.loaded = true
	data, err := os.ReadFile(c.path)
	if err != nil {
		return nil
	}
	if err := json.Unmarshal(data, &c.entries); err != nil {
		c.entries = nil
	}
	return c.entries
}

// writeAtomic writes entries to a tmp file in the same directory, then renames
// over the target. A crash mid-write leaves the previous file intact.
func (c *Cache) writeAtomic(entries []Entry) error {
	dir := filepath.Dir(c.path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".cache-*.json")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	enc := json.NewEncoder(tmp)
	if err := enc.Encode(entries); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Chmod(tmpName, 0600); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, c.path)
}
