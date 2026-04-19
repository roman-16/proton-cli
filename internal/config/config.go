// Package config loads and resolves ~/.config/proton-cli/config.toml.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// Profile is a set of credentials and endpoint overrides.
type Profile struct {
	User       string `toml:"user"`
	Password   string `toml:"password"`
	TOTP       string `toml:"totp"`
	APIURL     string `toml:"api_url"`
	AppVersion string `toml:"app_version"`
}

// Config is the full on-disk configuration.
type Config struct {
	DefaultProfile string             `toml:"default_profile"`
	Profiles       map[string]Profile `toml:"profiles"`
}

// Path returns ~/.config/proton-cli/config.toml.
func Path() (string, error) {
	cd, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cd, "proton-cli", "config.toml"), nil
}

// Load reads the config file. Missing file is not an error — an empty config
// with a "default" profile is returned instead.
func Load() (*Config, error) {
	p, err := Path()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &Config{DefaultProfile: "default", Profiles: map[string]Profile{}}, nil
		}
		return nil, err
	}
	var c Config
	if err := toml.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parse %s: %w", p, err)
	}
	if c.DefaultProfile == "" {
		c.DefaultProfile = "default"
	}
	if c.Profiles == nil {
		c.Profiles = map[string]Profile{}
	}
	return &c, nil
}

// Resolve returns the profile with the given name, or the default profile
// when name is empty. Missing profiles yield an empty (but non-nil) Profile.
func (c *Config) Resolve(name string) (string, Profile) {
	if name == "" {
		name = c.DefaultProfile
	}
	if name == "" {
		name = "default"
	}
	return name, c.Profiles[name]
}
