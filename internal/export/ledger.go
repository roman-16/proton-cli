package export

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/roman-16/proton-cli/internal/localfile"
)

type Entry struct {
	ID      string `json:"id"`
	Version string `json:"version,omitempty"`
	Path    string `json:"path,omitempty"`
	Want    string `json:"want,omitempty"`
	Gone    bool   `json:"gone,omitempty"`
}

func (e Entry) Done() bool { return e.Version != "" }

type Ledger struct {
	mu      sync.Mutex
	path    string
	copy    *Copy
	file    *os.File
	entries map[string]Entry
	taken   map[string]string
}

const maxLine = 1 << 20

func openLedger(at string, c *Copy) (*Ledger, error) {
	l := &Ledger{path: at, copy: c, entries: map[string]Entry{}, taken: map[string]string{}}
	raw, err := os.ReadFile(at)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte, 64*1024), maxLine)
	for scanner.Scan() {
		var e Entry
		if json.Unmarshal(scanner.Bytes(), &e) != nil || e.ID == "" {
			continue
		}
		l.apply(e)
	}
	if err := l.reopen(); err != nil {
		return nil, err
	}
	return l, nil
}

func (l *Ledger) reopen() error {
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	l.file = f
	return nil
}

func (l *Ledger) apply(e Entry) {
	if was, ok := l.entries[e.ID]; ok && was.Path != "" {
		delete(l.taken, fold(was.Path))
	}
	if e.Gone {
		delete(l.entries, e.ID)
		return
	}
	l.entries[e.ID] = e
	if e.Path != "" {
		l.taken[fold(e.Path)] = e.ID
	}
}

func fold(p string) string { return strings.ToLower(p) }

func (l *Ledger) Get(id string) (Entry, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	e, ok := l.entries[id]
	return e, ok
}

func (l *Ledger) Put(e Entry) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.put(e)
}

func (l *Ledger) put(e Entry) error {
	line, err := json.Marshal(e)
	if err != nil {
		return err
	}
	if _, err := l.file.Write(append(line, '\n')); err != nil {
		return err
	}
	l.apply(e)
	return nil
}

func (l *Ledger) Forget(id string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, ok := l.entries[id]; !ok {
		return nil
	}
	return l.put(Entry{ID: id, Gone: true})
}

func (l *Ledger) IDs() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]string, 0, len(l.entries))
	for id := range l.entries {
		out = append(out, id)
	}
	slices.Sort(out)
	return out
}

func (l *Ledger) Place(id, want string) (now, was string, err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	e, ok := l.entries[id]
	if ok && e.Want == want && e.Path != "" {
		return e.Path, e.Path, nil
	}
	taken := func(candidate string) bool {
		candidate = filepath.ToSlash(candidate)
		if owner, held := l.taken[fold(candidate)]; held && owner != id {
			return true
		}
		return candidate != e.Path && localfile.Exists(l.copy.Path(candidate))
	}
	free, err := localfile.FreeOf(want, taken)
	if err != nil {
		return "", "", err
	}
	free = filepath.ToSlash(free)
	version := ""
	if ok && e.Path != "" && localfile.Exists(l.copy.Path(e.Path)) {
		version = e.Version
	}
	if err := l.put(Entry{ID: id, Version: version, Path: free, Want: want}); err != nil {
		return "", "", err
	}
	return free, e.Path, nil
}

func (l *Ledger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := l.file.Close(); err != nil {
		return err
	}
	ids := make([]string, 0, len(l.entries))
	for id := range l.entries {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	var doc bytes.Buffer
	for _, id := range ids {
		line, err := json.Marshal(l.entries[id])
		if err != nil {
			return err
		}
		doc.Write(line)
		doc.WriteByte('\n')
	}
	return localfile.Publish(l.path, doc.Bytes(), time.Time{})
}
