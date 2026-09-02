package kit

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

// Where downloaded bytes go.
//
// Every command that writes a payload to disk answers the same three questions:
// which path, what if it exists, and can it stream to stdout instead. Answering
// them once means attachments, Drive files, photos and exports behave the same,
// and that `--force` means exactly one thing across the whole CLI.

// Destination is the --dest / --dest-dir / --force group.
//
// A local path is named by --dest and a remote container by --into, which is
// what keeps the two halves of "where does this go" from sharing a word: bytes
// land on this machine, a message lands in a folder.
type Destination struct {
	dest    string
	destDir string
	force   bool
}

func (d *Destination) Register(c *cobra.Command) {
	f := c.Flags()
	f.StringVar(&d.dest, "dest", "", "Write to this path, or - for stdout")
	f.StringVar(&d.destDir, "dest-dir", "", "Write into this directory, keeping each item's own name")
	f.BoolVar(&d.force, "force", false, "Overwrite a file that already exists")
}

// Validate rejects combinations that cannot mean anything, before any bytes are
// fetched. single says whether exactly one item is being written.
//
// A named path that is already taken is one of them: whether the file is there
// is a local fact, so finding out after the download would mean spending the
// whole transfer to learn something that was true before it started.
func (d *Destination) Validate(single bool) error {
	if d.dest != "" && d.destDir != "" {
		return Fail("--dest and --dest-dir cannot both be given.").
			Hint("--dest names one file; --dest-dir names a directory to fill.")
	}
	if err := d.free(d.dest); err != nil {
		return err
	}
	if !single && d.dest != "" {
		// Several separate files down one stream arrive as one unusable run of
		// bytes, so stdout is no more of an answer here than a single path is.
		names := "--dest names one file"
		if d.dest == "-" {
			names = "--dest - writes one stream"
		}
		return Fail("%s, but several items were selected.", names).
			Hint("use --dest-dir to write them all into a directory.")
	}
	return nil
}

// free reports that a named path may be written, which --force is the way to say
// about one that is taken. It is asked twice: once before the work, so a
// collision costs nothing, and again as the bytes are published, because the
// answer can change while a large payload is arriving.
func (d *Destination) free(path string) error {
	if path == "" || path == "-" || d.force || !exists(path) {
		return nil
	}
	return Fail("%s already exists.", path).Hint("--force to overwrite it.")
}

// Stdout reports whether the payload streams to standard output.
func (d *Destination) Stdout() bool { return d.dest == "-" }

// Describe names the destination for a confirmation.
func (d *Destination) Describe() string {
	switch {
	case d.dest == "-":
		return "stdout"
	case d.dest != "":
		return d.dest
	case d.destDir != "":
		return d.destDir
	}
	return "the current directory"
}

// Write puts data where the flags say, returning the path written, or "" when it
// streamed to stdout.
//
// The collision policy differs by intent, and deliberately so: an explicit
// --dest path is refused if it exists, because the user named that exact file
// and silently replacing it would destroy something they did not mention. A name
// the CLI chose itself gets a numbered suffix instead, because there was no
// promise to keep.
func (d *Destination) Write(c *Invocation, name string, data []byte) (string, error) {
	if d.dest == "-" {
		_, err := c.UI().Out.Write(data)
		return "", err
	}
	if d.dest != "" {
		if !d.force && exists(d.dest) {
			return "", Fail("%s already exists.", d.dest).Hint("--force to overwrite it.")
		}
		return d.dest, os.WriteFile(d.dest, data, 0o600)
	}
	if d.destDir != "" {
		if err := EnsureDir(d.destDir); err != nil {
			return "", err
		}
	}
	target := filepath.Join(d.destDir, SafeFilename(name))
	if !d.force {
		free, err := freePath(target)
		if err != nil {
			return "", err
		}
		target = free
	}
	return target, os.WriteFile(target, data, 0o600)
}

// Stream writes one payload without ever holding it whole in memory.
//
// It exists because a payload can be larger than the machine: an mbox of a whole
// mailbox, a Drive file of any size. File output goes to a temporary file beside
// the destination and is renamed once the producer is done, so a failure part way
// through leaves nothing behind rather than a plausible-looking truncated file.
//
// name is only consulted when the caller has no --dest path of its own.
func (d *Destination) Stream(c *Invocation, name string, write func(io.Writer) error) (string, error) {
	return d.StreamDiscovered(c, func(w io.Writer) (string, error) { return name, write(w) })
}

// StreamDiscovered is Stream for a payload whose own name only becomes known once
// it starts arriving, which is the case for a Drive photo: the producer returns
// the name it found.
func (d *Destination) StreamDiscovered(c *Invocation, write func(io.Writer) (string, error)) (string, error) {
	if d.dest == "-" {
		_, err := write(c.UI().Out)
		return "", err
	}
	dir, err := d.Dir()
	if err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(dir, ".proton-cli-*")
	if err != nil {
		return "", err
	}
	committed := false
	defer func() {
		_ = tmp.Close()
		if !committed {
			_ = os.Remove(tmp.Name())
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		return "", err
	}
	name, err := write(tmp)
	if err != nil {
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	target, err := d.Reserve(name)
	if err != nil {
		return "", err
	}
	if err := os.Rename(tmp.Name(), target); err != nil {
		return "", err
	}
	committed = true
	return target, nil
}

// EnsureDir creates a directory if it is missing, and refuses a path that exists
// as something else.
func EnsureDir(dir string) error {
	if fi, err := os.Stat(dir); err == nil {
		if !fi.IsDir() {
			return Fail("%s exists and is not a directory.", dir)
		}
		return nil
	}
	return os.MkdirAll(dir, 0o755)
}

const maxSuffix = 1000

// freePath returns the first unused name of the form "stem (2).ext".
func freePath(path string) (string, error) {
	if !exists(path) {
		return path, nil
	}
	dir, base := filepath.Split(path)
	stem, ext := splitExt(base)
	for i := 2; i <= maxSuffix; i++ {
		candidate := filepath.Join(dir, fmt.Sprintf("%s (%d)%s", stem, i, ext))
		if !exists(candidate) {
			return candidate, nil
		}
	}
	return "", Fail("could not find a free name next to %s.", path)
}

// splitExt splits on the last dot, so "archive.tar.gz" keeps ".gz" as its
// extension and "archive.tar" as its stem. A leading dot belongs to the stem, so
// ".bashrc" is not treated as an extension.
func splitExt(name string) (stem, ext string) {
	i := strings.LastIndexByte(name, '.')
	if i <= 0 {
		return name, ""
	}
	return name[:i], name[i:]
}

// SafeFilename turns arbitrary text - a mail subject, say - into a name every
// filesystem the CLI targets will accept.
func SafeFilename(s string) string {
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

func exists(path string) bool {
	_, err := os.Stat(path)
	if err == nil {
		return true
	}
	return !errors.Is(err, fs.ErrNotExist)
}

// ReadTextArg resolves a text flag that may be "-", meaning stdin. Centralising
// the convention is what makes `--body -`, `--sieve -`, `--message -` and
// `--signature -` all behave the same - and what lets stdin have one owner, so
// this and --password-stdin cannot quietly drain the same stream.
func ReadTextArg(c *Invocation, value, flag string) (string, error) {
	if value != "-" {
		return value, nil
	}
	r, err := c.App.Stdin(flag + " -")
	if err != nil {
		return "", err
	}
	b, err := io.ReadAll(r)
	if err != nil {
		return "", Fail("could not read %s from stdin: %v", flag, err)
	}
	return string(b), nil
}

// Reserve returns the path Write would use, without writing anything.
//
// Streaming downloads need the file open before the bytes arrive, so they cannot
// hand a finished buffer to Write. Reserving keeps them on the same collision
// policy as everything else instead of inventing their own.
func (d *Destination) Reserve(name string) (string, error) {
	if d.dest != "" {
		if err := d.free(d.dest); err != nil {
			return "", err
		}
		return d.dest, nil
	}
	if d.destDir != "" {
		if err := EnsureDir(d.destDir); err != nil {
			return "", err
		}
	}
	target := filepath.Join(d.destDir, SafeFilename(name))
	if d.force {
		return target, nil
	}
	return freePath(target)
}

// Dir is the directory a download will land in, for callers that must open a
// temporary file beside the final destination.
//
// It creates the directory, because a caller reaches for it before Reserve has
// had a chance to, and a temporary file cannot be opened in a directory that is
// not there yet.
func (d *Destination) Dir() (string, error) {
	switch {
	case d.dest != "":
		dir := filepath.Dir(d.dest)
		return dir, EnsureDir(dir)
	case d.destDir != "":
		return d.destDir, EnsureDir(d.destDir)
	}
	return ".", nil
}

// ReadBytesArg resolves a path argument that may be "-", meaning stdin, for
// something that is not text.
//
// An archive is bytes rather than a string, so reading one through ReadTextArg
// would put it through a conversion it does not survive on every platform.
func ReadBytesArg(c *Invocation, path, name string) ([]byte, error) {
	if path == "-" {
		r, err := c.App.Stdin(name + " -")
		if err != nil {
			return nil, err
		}
		b, err := io.ReadAll(r)
		if err != nil {
			return nil, Fail("could not read %s from stdin: %v", name, err)
		}
		return b, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, Fail("could not read %s: %v", path, err)
	}
	return b, nil
}
