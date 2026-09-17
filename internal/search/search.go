// Package search is the encrypted copy of an account this machine keeps so that
// what Proton cannot search can be searched from here.
//
// Proton's own search covers what the server can read: a subject, a sender, a
// recipient. A body, a file's name and an event's text are encrypted to the
// account's keys, so the only thing that can search them is something holding
// them decrypted - which is why every Proton client with content search keeps a
// copy of its own, and why this one does too.
//
// A command lives for one invocation, so the copy lives on disk: one directory
// per profile, one key, one append-only log per app. What is in the log is the
// account's, so nothing about a thing is written outside its ciphertext - not an
// ID, not a time, not a word of what it says. The files beside it hold counts
// and cursors, which describe a shape and name nothing.
package search

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// App is one of the account's apps, and one log in the directory.
type App string

const (
	AppCalendar App = "calendar"
	AppDrive    App = "drive"
	AppMail     App = "mail"
)

// Apps is every app an index can be built for.
var Apps = []App{AppCalendar, AppDrive, AppMail}

// Known reports whether a word names an app that can be indexed.
func Known(name string) (App, bool) {
	for _, a := range Apps {
		if string(a) == strings.ToLower(name) {
			return a, true
		}
	}
	return "", false
}

// Names is the apps as words, for a message that has to list them.
func Names(apps []App) []string {
	out := make([]string, 0, len(apps))
	for _, a := range apps {
		out = append(out, string(a))
	}
	return out
}

// ErrNotIndexed is what asking about an app with no index answers.
var ErrNotIndexed = errors.New("search: no index for this app")

// ErrOtherAccount is an index that was built by a different account, which
// nothing this session holds will open.
var ErrOtherAccount = errors.New("search: the index belongs to another account")

// ErrBusy is another run holding the directory. Only one process indexes at a
// time; everything else reads what is there.
var ErrBusy = errors.New("search: another run is indexing")

// Store is a profile's directory of indexes.
//
// It is constructed with the account it belongs to and a way to reach the keys,
// rather than reaching for either itself: what unlocks an account is the account
// layer's business, and this is a file format.
type Store struct {
	dir string
	// account is the user ID the session holds, read when it is needed because a
	// sign-in during the run is what changes it.
	account func() string
	unlock  Unlock

	key []byte
}

// Keys is what the account offers an index: the key a new one is sealed to, and
// every key that may open one.
//
// They differ for the reason they differ everywhere else in this CLI. Sealing
// goes under the primary key, because that is the current one; opening goes
// under all of them, because an index sealed before a key rotation still has to
// open afterwards.
type Keys struct {
	Seal *KeyRing
	Open *KeyRing
}

// Unlock reaches the account's keys.
type Unlock func(context.Context) (Keys, error)

// New is the store for one profile's directory.
func New(dir string, account func() string, unlock Unlock) *Store {
	return &Store{dir: dir, account: account, unlock: unlock}
}

// Dir is where the indexes are kept, which a report and the settings page name.
func (s *Store) Dir() string { return s.dir }

func (s *Store) logPath(app App) string   { return filepath.Join(s.dir, string(app)+".bin") }
func (s *Store) statePath(app App) string { return filepath.Join(s.dir, string(app)+".json") }
func (s *Store) keyPath() string          { return filepath.Join(s.dir, "key") }

// Exists reports whether this app has an index on disk.
func (s *Store) Exists(app App) bool {
	_, err := os.Stat(s.statePath(app))
	return err == nil
}

// Status is what one index is, as `index list` shows it.
type Status struct {
	App App `json:"app"`
	// Indexed and Total are how much of the app is in the log, and how much
	// there was to index when the build last looked.
	Indexed int `json:"indexed"`
	Total   int `json:"total"`
	// Unreadable is how many things went in without their content, because the
	// content would not open.
	Unreadable int `json:"unreadable,omitempty"`
	// Bodies is how many of the indexed things hold the text a search reads,
	// where holding it is a request of its own. Mail is the app that is: a
	// message is indexed by its envelope first and by its body afterwards, so a
	// count short of Indexed is bodies still being downloaded.
	Bodies int `json:"bodies"`
	// Complete says the first build finished, so nothing is missing but what has
	// happened since.
	Complete bool `json:"complete"`
	// Stale says Proton could not say what has changed, so the index is owed a
	// reading of the account rather than of its change feed.
	Stale bool `json:"stale,omitempty"`
	// Oldest is the far end of what has been indexed, which is what says how far
	// back a search over a half-built index reaches.
	Oldest  int64 `json:"oldest,omitempty"`
	Updated int64 `json:"updated"`
	Bytes   int64 `json:"bytes"`
	// Volume is the container the index was built from, for an app that has
	// more than one and can only answer for the one it holds.
	Volume string `json:"volume,omitempty"`
}

// List is every index in the directory, alphabetically by app.
//
// It reads the files and nothing else: what is on this machine is a question
// this machine can answer, so it answers it signed out.
func (s *Store) List() ([]Status, error) {
	var out []Status
	for _, app := range Apps {
		st, err := s.Status(app)
		if errors.Is(err, ErrNotIndexed) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, nil
}

// Status is what one index holds.
func (s *Store) Status(app App) (Status, error) {
	st, err := s.state(app)
	if err != nil {
		return Status{}, err
	}
	return statusOf(app, st, s.bytes(app)), nil
}

// statusOf is one index's state as it is reported, whether it was read off the
// file beside the log or off the log a run has open.
func statusOf(app App, st State, bytes int64) Status {
	return Status{
		App: app, Indexed: st.Indexed, Total: st.Total, Unreadable: st.Unreadable,
		Bodies: st.Bodies, Complete: st.Complete, Stale: st.Stale,
		Oldest: st.Oldest, Updated: st.Updated, Bytes: bytes, Volume: st.Volume,
	}
}

// bytes is how much disk one index takes, which is what a removal is worth
// saying out loud.
func (s *Store) bytes(app App) int64 {
	var total int64
	for _, p := range []string{s.logPath(app), s.statePath(app)} {
		if info, err := os.Stat(p); err == nil {
			total += info.Size()
		}
	}
	return total
}

// Delete removes one app's index, and the key with the last of them.
//
// The key opens nothing once the logs are gone, and leaving it behind would
// leave the directory looking like an index that had lost its contents.
func (s *Store) Delete(app App) error {
	for _, p := range []string{s.logPath(app), s.statePath(app)} {
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	left, err := s.List()
	if err != nil {
		return err
	}
	if len(left) > 0 {
		return nil
	}
	if err := os.Remove(s.keyPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Remove(lockPath(s.dir)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Remove(s.dir); err != nil && !errors.Is(err, os.ErrNotExist) && !isNotEmpty(err) {
		return err
	}
	return nil
}

func isNotEmpty(err error) bool {
	return errors.Is(err, os.ErrExist) || strings.Contains(err.Error(), "not empty")
}

// mine reports that an index belongs to the account this session is signed in
// as. An index of somebody else's mail is one nothing here opens, and saying so
// beats a decryption failure on every record.
func (s *Store) mine(st State) error {
	account := ""
	if s.account != nil {
		account = s.account()
	}
	if st.Account == "" || account == "" || st.Account == account {
		return nil
	}
	return ErrOtherAccount
}

func (s *Store) ensureDir() error {
	if err := os.MkdirAll(s.dir, 0700); err != nil {
		return fmt.Errorf("search: create %s: %w", s.dir, err)
	}
	return nil
}
