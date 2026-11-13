package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/roman-16/proton-cli/internal/config"
	"github.com/roman-16/proton-cli/internal/localfile"
	"github.com/roman-16/proton-cli/internal/profile"
	"github.com/roman-16/proton-cli/internal/skip"
)

type Session struct {
	UID          string `json:"uid"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	UserID       string `json:"user_id,omitempty"`
	Email        string `json:"email,omitempty"`
	EncKeyBlob   string `json:"enc_key_blob,omitempty"`
	PersistedAt  int64  `json:"persisted_at,omitempty"`
	BaseURL      string `json:"base_url"`
}

var ErrDamaged = errors.New("the saved session is damaged")

const (
	holdPoll   = 100 * time.Millisecond
	holdNotice = time.Second
)

func (s *Session) Unlocked() bool { return s != nil && s.EncKeyBlob != "" }

type Profile struct {
	Name        string `json:"name"`
	Email       string `json:"email,omitempty"`
	Unlocked    bool   `json:"unlocked"`
	PersistedAt int64  `json:"persisted_at,omitempty"`
}

func Profiles(ctx context.Context) ([]Profile, error) {
	d, err := Dir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(filepath.Join(d, "sessions"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var out []Profile
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		name, err := profile.Parse(strings.TrimSuffix(e.Name(), ".json"))
		if err != nil {
			skip.Record(ctx, skip.KindProfile, e.Name(), skip.Malformed, err)
			continue
		}
		s, err := Load(name)
		switch {
		case errors.Is(err, ErrDamaged):
			skip.Record(ctx, skip.KindProfile, name.String(), skip.Malformed, err)
			continue
		case err != nil:
			skip.Record(ctx, skip.KindProfile, name.String(), skip.Unreadable, err)
			continue
		case s == nil:
			continue
		}
		out = append(out, Profile{
			Name: name.String(), Email: s.Email,
			Unlocked: s.Unlocked(), PersistedAt: s.PersistedAt,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func Dir() (string, error) { return config.Dir() }

func Path(name profile.Name) (string, error) {
	d, err := Dir()
	if err != nil {
		return "", err
	}
	return pathIn(d, name, ".json"), nil
}

func pathIn(d string, name profile.Name, ext string) string {
	return filepath.Join(d, "sessions", name.FileName(ext))
}

func Load(name profile.Name) (*Session, error) {
	p, err := Path(name)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var s Session
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDamaged, err)
	}
	if s.UID == "" || s.AccessToken == "" || s.RefreshToken == "" || s.BaseURL == "" {
		return nil, ErrDamaged
	}
	return &s, nil
}

func Save(name profile.Name, s *Session) error {
	s.PersistedAt = time.Now().Unix()
	p, err := Path(name)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		return err
	}
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(p), "."+filepath.Base(p)+".*")
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
	return os.Rename(tmp, p)
}

func Clear(name profile.Name) error {
	p, err := Path(name)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func Hold(ctx context.Context, name profile.Name) (func(), error) {
	d, err := Dir()
	if err != nil {
		return nil, err
	}
	p := pathIn(d, name, ".lock")
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		return nil, err
	}
	started := time.Now()
	told := false
	for {
		lock, err := localfile.Claim(p)
		if err == nil {
			return lock.Release, nil
		}
		if !errors.Is(err, localfile.ErrBusy) {
			return nil, err
		}
		if !told && time.Since(started) >= holdNotice {
			slog.WarnContext(ctx, "Waiting for another command that is using this session.")
			told = true
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(holdPoll):
		}
	}
}

func DropLock(name profile.Name) error {
	d, err := Dir()
	if err != nil {
		return err
	}
	if err := os.Remove(pathIn(d, name, ".lock")); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
