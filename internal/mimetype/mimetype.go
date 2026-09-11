// Package mimetype decides what a file is, so that a type stored beside somebody's
// bytes is the one Proton's own clients would have stored.
//
// There are two rules rather than one, and which applies is the app's business:
// Drive and Mail type a file by its name, and Pass types it by looking at it.
package mimetype

import (
	"mime"
	"net/http"
	"path/filepath"
	"strings"
)

// Unknown is what a file nothing recognises is called.
const Unknown = "application/octet-stream"

// protonExtensions are the types Proton names itself, because the table
// everything else comes from either disagrees or is silent.
var protonExtensions = map[string]string{
	"jxl": "image/jxl",
	"py":  "text/x-python",
	"ts":  "application/typescript",
}

// rawExtensions are the camera formats Proton recognises by extension. No
// general table carries them, and a photograph landing as an unknown blob is
// the thing this exists to prevent.
var rawExtensions = map[string]string{
	"3fr":   "image/x-hasselblad-3fr",
	"arw":   "image/x-sony-arw",
	"cr2":   "image/x-canon-cr2",
	"cr3":   "image/x-canon-cr3",
	"crw":   "image/x-canon-crw",
	"dcr":   "image/x-kodak-dcr",
	"dcraw": "image/x-dcraw",
	"dng":   "image/x-adobe-dng",
	"erf":   "image/x-epson-erf",
	"fff":   "image/x-hasselblad-fff",
	"iiq":   "image/x-phaseone-iiq",
	"k25":   "image/x-kodak-k25",
	"kdc":   "image/x-kodak-kdc",
	"mef":   "image/x-mamiya-mef",
	"mrw":   "image/x-minolta-mrw",
	"nef":   "image/x-nikon-nef",
	"nrw":   "image/x-nikon-nrw",
	"orf":   "image/x-olympus-orf",
	"pef":   "image/x-pentax-pef",
	"ptx":   "image/x-pentax-ptx",
	"raf":   "image/x-fuji-raf",
	"raw":   "image/x-panasonic-raw",
	"rw2":   "image/x-panasonic-rw2",
	"rwl":   "image/x-leica-rwl",
	"sr2":   "image/x-sony-sr2",
	"srf":   "image/x-sony-srf",
	"x3f":   "image/x-sigma-x3f",
}

// ByName is what a file called this is, which is how Drive and Mail decide.
//
// A name that says nothing gets Unknown, never an empty string: a type is stored
// with the file and something has to be stored.
func ByName(name string) string {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(name), "."))
	if ext == "" {
		return Unknown
	}
	if t, ok := protonExtensions[ext]; ok {
		return t
	}
	if t := mime.TypeByExtension("." + ext); t != "" {
		return bare(t)
	}
	if t, ok := rawExtensions[ext]; ok {
		return t
	}
	return Unknown
}

// ByContent is what the start of a file says it is, which is how Pass decides.
//
// head is the first of the bytes; the rest are not needed and need never be read
// into memory. Where the content settles nothing - anything textual, anything
// unrecognised - the name is asked instead, so a .csv is a .csv and a file with
// no extension is still whatever it turns out to be.
func ByContent(head []byte, name string) string {
	sniffed := bare(http.DetectContentType(head))
	if sniffed != Unknown && !strings.HasPrefix(sniffed, "text/plain") {
		return sniffed
	}
	if named := ByName(name); named != Unknown {
		return named
	}
	return sniffed
}

// bare drops the parameters a type carries, leaving the type itself. Proton
// stores "text/plain" rather than "text/plain; charset=utf-8", and a type with a
// charset bolted on sorts and compares as a different type.
func bare(t string) string {
	if parsed, _, err := mime.ParseMediaType(t); err == nil {
		return parsed
	}
	return t
}
