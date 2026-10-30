package localfile

import (
	"errors"
	"os"
)

var ErrBusy = errors.New("localfile: another process holds the lock")

type Lock struct{ file *os.File }

func Claim(path string) (*Lock, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := lockFile(f); err != nil {
		_ = f.Close()
		return nil, err
	}
	return &Lock{file: f}, nil
}

func (l *Lock) Release() {
	if l == nil || l.file == nil {
		return
	}
	_ = unlockFile(l.file)
	_ = l.file.Close()
	l.file = nil
}
