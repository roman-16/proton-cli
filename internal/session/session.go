// Package session persists per-profile Proton auth state.
package session

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// Session holds the persisted auth state for one profile.
type Session struct {
	UID           string `json:"uid"`
	AccessToken   string `json:"access_token"`
	RefreshToken  string `json:"refresh_token"`
	SaltedKeyPass string `json:"salted_key_pass,omitempty"`
	AppVersion    string `json:"app_version"`
	BaseURL       string `json:"base_url"`
}

// Dir returns ~/.config/proton-cli.
func Dir() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "proton-cli"), nil
}

// Path returns the session-file path for the given profile.
// Default profile falls back to the legacy ~/.config/proton-cli/session.json
// if present (transparent migration).
func Path(profile string) (string, error) {
	d, err := Dir()
	if err != nil {
		return "", err
	}
	if profile == "" {
		profile = "default"
	}
	newPath := filepath.Join(d, "sessions", profile+".json")
	if profile == "default" {
		if _, err := os.Stat(newPath); err == nil {
			return newPath, nil
		}
		legacy := filepath.Join(d, "session.json")
		if _, err := os.Stat(legacy); err == nil {
			return legacy, nil
		}
	}
	return newPath, nil
}

// Load reads the session for the given profile. Returns nil (no error) when
// no session file exists yet.
func Load(profile string) (*Session, error) {
	p, err := Path(profile)
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

// Save writes the session for the given profile to disk.
func Save(profile string, s *Session) error {
	if profile == "" {
		profile = "default"
	}
	d, err := Dir()
	if err != nil {
		return err
	}
	newPath := filepath.Join(d, "sessions", profile+".json")
	if err := os.MkdirAll(filepath.Dir(newPath), 0700); err != nil {
		return err
	}
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return os.WriteFile(newPath, data, 0600)
}

// Clear removes the session file for the given profile.
func Clear(profile string) error {
	p, err := Path(profile)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
