package search

import (
	"os"
	"path/filepath"
)

// One process writes to a profile's indexes at a time.
//
// Reading needs no lock: a record is sealed and framed, so a reader either sees
// a whole one or stops at the first that is not there yet, and a build in
// progress is a shorter index rather than a broken one. That is what lets a
// search answer from an index while `index watch` keeps it current beside it.
//
// Writing is the part that has to be alone, and not because two appends would
// tear each other: they would fetch the same messages twice and write them
// twice. So a run that would write takes the lock, and one that cannot have it
// reads what is there - whatever holds it is keeping the index current anyway.

// Lock is a held claim on a profile's indexes.
type Lock struct{ file *os.File }

func lockPath(dir string) string { return filepath.Join(dir, "lock") }

// Claim takes the directory for writing, or answers ErrBusy.
func (s *Store) Claim() (*Lock, error) {
	if err := s.ensureDir(); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(lockPath(s.dir), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := lockFile(f); err != nil {
		_ = f.Close()
		return nil, err
	}
	return &Lock{file: f}, nil
}

// Release gives the claim up. A lock is a file handle, so a run that ends
// without releasing one loses it anyway.
func (l *Lock) Release() {
	if l == nil || l.file == nil {
		return
	}
	_ = unlockFile(l.file)
	_ = l.file.Close()
	l.file = nil
}
