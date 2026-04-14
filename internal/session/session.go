package session

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// Session holds the persisted auth state.
type Session struct {
	UID           string `json:"uid"`
	AccessToken   string `json:"access_token"`
	RefreshToken  string `json:"refresh_token"`
	SaltedKeyPass string `json:"salted_key_pass,omitempty"`
	AppVersion    string `json:"app_version"`
	BaseURL       string `json:"base_url"`
}

func path() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "proton-cli", "session.json"), nil
}

// Load reads the session from disk. Returns nil if no session exists.
func Load() (*Session, error) {
	p, err := path()
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

// Save writes the session to disk.
func Save(s *Session) error {
	p, err := path()
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

	return os.WriteFile(p, data, 0600)
}

// Clear removes the session file.
func Clear() error {
	p, err := path()
	if err != nil {
		return err
	}

	err = os.Remove(p)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
