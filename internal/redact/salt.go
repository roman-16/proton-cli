package redact

import (
	"crypto/rand"
	"os"
	"path/filepath"
)

// saltLength is 32 bytes, the block size of the hash the handles are derived
// with. More would be folded; less would be a shorter key for no gain.
const saltLength = 32

// SaltName is the file the salt lives in, inside whatever directory the log
// does.
const SaltName = "salt"

// Salt reads the machine's salt from dir, creating it the first time.
//
// It never fails in a way a caller has to handle: a directory that cannot be
// written, a file that cannot be read, a salt of the wrong length all yield a
// fresh random salt held only in memory. The consequence is that handles stop
// being stable across runs, which costs a reader some continuity between two
// reports and costs nobody their privacy - so it is the right way to fail.
func Salt(dir string) []byte {
	path := filepath.Join(dir, SaltName)
	if existing, err := os.ReadFile(path); err == nil && len(existing) == saltLength {
		return existing
	}
	fresh := make([]byte, saltLength)
	if _, err := rand.Read(fresh); err != nil {
		return nil
	}
	if err := os.MkdirAll(dir, 0o700); err == nil {
		_ = os.WriteFile(path, fresh, 0o600)
	}
	return fresh
}
