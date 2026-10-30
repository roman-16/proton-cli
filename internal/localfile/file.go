package localfile

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/roman-16/proton-cli/internal/errs"
)

const tempPattern = ".proton-cli-*"

func Publish(path string, data []byte, modified time.Time) error {
	return Stream(path, modified, func(w io.Writer) error {
		_, err := w.Write(data)
		return err
	})
}

func Stream(path string, modified time.Time, write func(io.Writer) error) error {
	dir := filepath.Dir(path)
	if err := EnsureDir(dir); err != nil {
		return err
	}
	tmp, err := Temp(dir)
	if err != nil {
		return err
	}
	return Commit(tmp, path, modified, write)
}

func Commit(tmp *os.File, path string, modified time.Time, write func(io.Writer) error) error {
	return CommitTo(tmp, func(w io.Writer) (string, time.Time, error) {
		return path, modified, write(w)
	})
}

func CommitTo(tmp *os.File, produce func(io.Writer) (string, time.Time, error)) error {
	committed := false
	defer func() {
		_ = tmp.Close()
		if !committed {
			_ = os.Remove(tmp.Name())
		}
	}()
	path, modified, err := produce(tmp)
	if err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := Stamp(tmp.Name(), modified); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return err
	}
	committed = true
	return nil
}

func Temp(dir string) (*os.File, error) {
	f, err := os.CreateTemp(dir, tempPattern)
	if err != nil {
		return nil, err
	}
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
		return nil, err
	}
	return f, nil
}

func Stamp(path string, modified time.Time) error {
	if modified.IsZero() {
		return nil
	}
	return os.Chtimes(path, modified, modified)
}

func Holds(path string, size int64, modified time.Time) bool {
	fi, err := os.Stat(path)
	if err != nil || !fi.Mode().IsRegular() || modified.IsZero() {
		return false
	}
	if size >= 0 && fi.Size() != size {
		return false
	}
	// FAT keeps a file's time in steps of two seconds.
	gap := fi.ModTime().Sub(modified)
	return gap < 2*time.Second && gap > -2*time.Second
}

func Sweep(dir string) error {
	return filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if d.Type().IsRegular() {
			if ok, _ := filepath.Match(tempPattern, d.Name()); ok {
				return os.Remove(path)
			}
		}
		return nil
	})
}

func EnsureDir(dir string) error {
	if fi, err := os.Stat(dir); err == nil {
		if !fi.IsDir() {
			return errs.Problemf("%s exists and is not a directory.", dir)
		}
		return nil
	}
	return os.MkdirAll(dir, 0o755)
}

const maxSuffix = 1000

func Free(path string) (string, error) {
	return FreeOf(path, Exists)
}

func FreeOf(path string, taken func(string) bool) (string, error) {
	if !taken(path) {
		return path, nil
	}
	dir, base := filepath.Split(path)
	stem, ext := splitExt(base)
	for i := 2; i <= maxSuffix; i++ {
		candidate := filepath.Join(dir, fmt.Sprintf("%s (%d)%s", stem, i, ext))
		if !taken(candidate) {
			return candidate, nil
		}
	}
	return "", errs.Problemf("could not find a free name next to %s.", path)
}

func splitExt(name string) (stem, ext string) {
	i := strings.LastIndexByte(name, '.')
	if i <= 0 {
		return name, ""
	}
	return name[:i], name[i:]
}

func SafeName(s string) string {
	const maxLen = 120
	var b strings.Builder
	for _, r := range s {
		switch {
		case strings.ContainsRune(`/\:*?"<>|`, r), r < 0x20:
			b.WriteByte(' ')
		default:
			b.WriteRune(r)
		}
	}
	out := strings.Join(strings.Fields(b.String()), " ")
	if len(out) > maxLen {
		out = strings.TrimSpace(out[:maxLen])
	}
	// A trailing dot makes a file unopenable on Windows.
	out = strings.TrimRight(out, ".")
	if out == "" {
		return "download"
	}
	return out
}

func Exists(path string) bool {
	_, err := os.Lstat(path)
	if err == nil {
		return true
	}
	return !errors.Is(err, fs.ErrNotExist)
}

func Room(path string) (int64, error) {
	for {
		if _, err := os.Stat(path); err == nil {
			return room(path)
		}
		parent := filepath.Dir(path)
		if parent == path {
			return room(path)
		}
		path = parent
	}
}
