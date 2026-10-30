package search

import (
	"path/filepath"

	"github.com/roman-16/proton-cli/internal/localfile"
)

func (s *Store) Claim() (*localfile.Lock, error) {
	if err := s.ensureDir(); err != nil {
		return nil, err
	}
	return localfile.Claim(s.lockPath())
}

func (s *Store) lockPath() string { return filepath.Join(s.dir, "lock") }
