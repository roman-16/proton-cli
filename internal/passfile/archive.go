package passfile

import (
	"archive/zip"
	"errors"
	"io"
	"strings"
)

// Reading the zip an export arrived in.
//
// Four of the managers write one, and all four want the same two things of it:
// the file with a given name, wherever in the tree it sits, and that file's
// contents whole.

// maxEntry is how much of one file inside an archive is read into memory.
//
// A compressed archive says nothing trustworthy about what it expands to, so
// what bounds the read is the budget rather than the size the file claims.
const maxEntry = 1 << 30

// errTooLarge is what a stream over the budget answers, for the caller to state
// in terms of the file the person named.
var errTooLarge = errors.New("over the budget")

// readCapped reads a stream whole, up to a budget.
//
// It reads one byte past the budget, because stopping at it exactly leaves a
// file short by an unknown amount - and a truncated export is indistinguishable
// downstream from one written by another program, so the run ends by blaming the
// format instead of saying what happened.
func readCapped(r io.Reader, budget int) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(r, int64(budget)+1))
	if err != nil {
		return nil, err
	}
	if len(body) > budget {
		return nil, errTooLarge
	}
	return body, nil
}

// readZipEntry is one file out of the archive, whole.
func readZipEntry(in source, f *zip.File) ([]byte, error) {
	r, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer func() { _ = r.Close() }()
	body, err := readCapped(r, maxEntry)
	if errors.Is(err, errTooLarge) {
		return nil, tooLarge(in)
	}
	return body, err
}

// zipEntry is the file with this name, whichever folder holds it.
func zipEntry(z *zip.ReadCloser, name string) *zip.File {
	for _, f := range z.File {
		if strings.EqualFold(zipBase(f.Name), name) {
			return f
		}
	}
	return nil
}

// zipBase is an entry's own name, without the folders above it.
func zipBase(name string) string {
	return name[strings.LastIndexAny(name, `/\`)+1:]
}
