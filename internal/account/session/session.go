// Package session persists per-profile Proton auth state.
package session

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/roman-16/proton-cli/internal/config"
	"github.com/roman-16/proton-cli/internal/profile"
	"github.com/roman-16/proton-cli/internal/skip"
)

// Session is what one profile keeps on disk between runs.
//
// The fields mirror WebClients' DefaultPersistedSession
// (packages/shared/lib/authentication/SessionInterface.ts): the tokens, the user
// it belongs to, when it was written, and the sealed key password. Email is the
// one addition, so listing profiles does not need an API call per profile.
type Session struct {
	UID          string `json:"uid"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	UserID       string `json:"user_id,omitempty"`
	Email        string `json:"email,omitempty"`
	// EncKeyBlob is the salted key password encrypted (AES-256-GCM) with a random
	// client key that lives server-side (PUT/GET /auth/v4/sessions/local/key).
	// Recovering the key password requires fetching that client key, so revoking
	// the session renders this blob undecryptable.
	EncKeyBlob  string `json:"enc_key_blob,omitempty"`
	PersistedAt int64  `json:"persisted_at,omitempty"`
	AppVersion  string `json:"app_version"`
	BaseURL     string `json:"base_url"`
}

// Unlocked reports whether the session can decrypt content without the account
// password, which it can once the key password has been sealed into it.
func (s *Session) Unlocked() bool { return s != nil && s.EncKeyBlob != "" }

// Profile is one named session slot on this machine.
type Profile struct {
	Name        string `json:"name"`
	Email       string `json:"email,omitempty"`
	Unlocked    bool   `json:"unlocked"`
	PersistedAt int64  `json:"persisted_at,omitempty"`
}

// Profiles lists the profiles that have a saved session, sorted by name.
//
// It reads the directory rather than any registry: the files are the state, so
// there is nothing that can disagree with them.
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
		if err != nil || s == nil {
			skip.Record(ctx, skip.KindProfile, name.String(), skip.Unreadable, err)
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

// Dir returns ~/.config/proton-cli.
func Dir() (string, error) { return config.Dir() }

// Path returns the session-file path for the given profile.
func Path(name profile.Name) (string, error) {
	d, err := Dir()
	if err != nil {
		return "", err
	}
	return pathIn(d, name), nil
}

// pathIn resolves the session-file path within base config dir d. Split out from
// Path so it can be tested against a temp dir.
//
// The name is a single path element by construction, which is why this can join
// without checking: a profile.Name cannot hold a separator or a relative segment.
func pathIn(d string, name profile.Name) string {
	return filepath.Join(d, "sessions", name.FileName(".json"))
}

// Load reads the session for the given profile. Returns nil (no error) when
// no session file exists yet.
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
		return nil, nil
	}
	if s.UID == "" || s.AccessToken == "" || s.RefreshToken == "" {
		return nil, nil
	}
	return &s, nil
}

// Save writes the session for the given profile.
//
// The file is replaced rather than rewritten in place, so a reader never sees a
// half-written session: a command that ran while another was saving would
// otherwise find a truncated file and conclude nobody is signed in.
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
