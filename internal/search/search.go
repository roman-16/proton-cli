package search

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type App string

const (
	AppCalendar App = "calendar"
	AppDrive    App = "drive"
	AppMail     App = "mail"
)

var Apps = []App{AppCalendar, AppDrive, AppMail}

func Known(name string) (App, bool) {
	for _, a := range Apps {
		if string(a) == strings.ToLower(name) {
			return a, true
		}
	}
	return "", false
}

func Names(apps []App) []string {
	out := make([]string, 0, len(apps))
	for _, a := range apps {
		out = append(out, string(a))
	}
	return out
}

var ErrNotIndexed = errors.New("search: no index for this app")

var ErrOtherAccount = errors.New("search: the index belongs to another account")

type Store struct {
	dir     string
	account func() string
	unlock  Unlock

	key []byte
}

type Keys struct {
	Seal *KeyRing
	Open *KeyRing
}

type Unlock func(context.Context) (Keys, error)

func New(dir string, account func() string, unlock Unlock) *Store {
	return &Store{dir: dir, account: account, unlock: unlock}
}

func (s *Store) Dir() string { return s.dir }

func (s *Store) logPath(app App) string   { return filepath.Join(s.dir, string(app)+".bin") }
func (s *Store) statePath(app App) string { return filepath.Join(s.dir, string(app)+".json") }
func (s *Store) keyPath() string          { return filepath.Join(s.dir, "key") }

func (s *Store) Exists(app App) bool {
	_, err := os.Stat(s.statePath(app))
	return err == nil
}

type Status struct {
	App        App    `json:"app"`
	Indexed    int    `json:"indexed"`
	Total      int    `json:"total"`
	Unreadable int    `json:"unreadable,omitempty"`
	Bodies     int    `json:"bodies"`
	Texts      int    `json:"texts"`
	Complete   bool   `json:"complete"`
	Stale      bool   `json:"stale,omitempty"`
	Oldest     int64  `json:"oldest,omitempty"`
	Updated    int64  `json:"updated"`
	Bytes      int64  `json:"bytes"`
	Volume     string `json:"volume,omitempty"`
}

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

func (s *Store) Status(app App) (Status, error) {
	st, err := s.state(app)
	if err != nil {
		return Status{}, err
	}
	return statusOf(app, st, s.bytes(app)), nil
}

func statusOf(app App, st State, bytes int64) Status {
	return Status{
		App: app, Indexed: st.Indexed, Total: st.Total, Unreadable: st.Unreadable,
		Bodies: st.Bodies, Texts: st.Texts, Complete: st.Complete, Stale: st.Stale,
		Oldest: st.Oldest, Updated: st.Updated, Bytes: bytes, Volume: st.Volume,
	}
}

func (s *Store) bytes(app App) int64 {
	var total int64
	for _, p := range []string{s.logPath(app), s.statePath(app)} {
		if info, err := os.Stat(p); err == nil {
			total += info.Size()
		}
	}
	return total
}

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
	if err := os.Remove(s.lockPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
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
