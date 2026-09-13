package passfile

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf16"
)

// ArchiveEntry is what a file is called inside an archive.
//
// Two items may hold files of the same name, and an archive is flat, so the name
// carries where the file came from. It is the app's own naming, because the
// archive is the app's: what this writes, Proton Pass reads back.
func ArchiveEntry(shareID, fileID, name string) string {
	base, ext := fileParts(name)
	return fmt.Sprintf("%s.%s%s%s", base, hashCode(shareID), hashCode(fileID), ext)
}

// ArchiveEntryName recovers the name a file had before an archive renamed it.
func ArchiveEntryName(path string) string {
	name := path
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	base, ext := fileParts(name)
	// A file that had no extension of its own was exported with the whole stamp
	// as one, which is what a run of that length is.
	if len(ext) >= 16 {
		return base
	}
	parts := strings.Split(base, ".")
	if len(parts) == 1 {
		return name
	}
	return strings.Join(parts[:len(parts)-1], ".") + ext
}

// fileParts splits a name on its last dot, the way the app does: a name with no
// dot is all base, and the extension keeps its dot.
func fileParts(name string) (base, ext string) {
	i := strings.LastIndexByte(name, '.')
	if i < 0 {
		return name, ""
	}
	return name[:i], name[i:]
}

// hashCode is the stamp an export puts in a file's name: Java's string hash, as
// the app computes it, in hexadecimal.
func hashCode(s string) string {
	var h int32
	for _, c := range utf16.Encode([]rune(s)) {
		h = h*31 + int32(c)
	}
	// The smallest int32 has no positive counterpart, so the absolute value is
	// taken with room to hold it - which is what the app's own arithmetic does.
	n := int64(h)
	if n < 0 {
		n = -n
	}
	return strconv.FormatInt(n, 16)
}
