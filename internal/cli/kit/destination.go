package kit

import (
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/roman-16/proton-cli/internal/localfile"
	"github.com/spf13/cobra"
)

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

func (d *Destination) Validate(single bool) error {
	if d.dest != "" && d.destDir != "" {
		return Fail("--dest and --dest-dir cannot both be given.").
			Hint("--dest names one file; --dest-dir names a directory to fill.")
	}
	if err := d.free(d.dest); err != nil {
		return err
	}
	if !single && d.dest != "" {
		names := "--dest names one file"
		if d.dest == "-" {
			names = "--dest - writes one stream"
		}
		return Fail("%s, but several items were selected.", names).
			Hint("use --dest-dir to write them all into a directory.")
	}
	return nil
}

func (d *Destination) free(path string) error {
	if path == "" || path == "-" || d.force || !localfile.Exists(path) {
		return nil
	}
	return Fail("%s already exists.", path).Hint("--force to overwrite it.")
}

func (d *Destination) Force() bool { return d.force }

func (d *Destination) Stdout() bool { return d.dest == "-" }

func WriteNamed(path string, data []byte, force bool) (string, error) {
	if !force && localfile.Exists(path) {
		return "", Fail("%s already exists.", path).Hint("--force to overwrite it.")
	}
	return path, localfile.Publish(path, data, time.Time{})
}

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

func (d *Destination) Where(name string) (string, error) {
	switch {
	case d.dest == "-":
		return "stdout", nil
	case d.dest != "":
		return d.dest, nil
	}
	target := filepath.Join(d.destDir, localfile.SafeName(name))
	if d.force {
		return target, nil
	}
	return localfile.Free(target)
}

func (d *Destination) Write(c *Invocation, name string, data []byte) (string, error) {
	if d.dest == "-" {
		_, err := c.UI().Out.Write(data)
		return "", err
	}
	if d.dest != "" {
		return WriteNamed(d.dest, data, d.force)
	}
	target := filepath.Join(d.destDir, localfile.SafeName(name))
	if !d.force {
		free, err := localfile.Free(target)
		if err != nil {
			return "", err
		}
		target = free
	}
	return target, localfile.Publish(target, data, time.Time{})
}

func (d *Destination) Stream(c *Invocation, name string, write func(io.Writer) error) (string, error) {
	return d.StreamDiscovered(c, func(w io.Writer) (string, error) { return name, write(w) })
}

func (d *Destination) StreamDiscovered(c *Invocation, write func(io.Writer) (string, error)) (string, error) {
	return d.StreamStamped(c, func(w io.Writer) (string, time.Time, error) {
		name, err := write(w)
		return name, time.Time{}, err
	})
}

func (d *Destination) StreamStamped(c *Invocation, write func(io.Writer) (string, time.Time, error)) (string, error) {
	if d.dest == "-" {
		_, _, err := write(c.UI().Out)
		return "", err
	}
	dir, err := d.Dir()
	if err != nil {
		return "", err
	}
	tmp, err := localfile.Temp(dir)
	if err != nil {
		return "", err
	}
	var target string
	err = localfile.CommitTo(tmp, func(w io.Writer) (string, time.Time, error) {
		name, modified, err := write(w)
		if err != nil {
			return "", time.Time{}, err
		}
		target, err = d.Reserve(name)
		return target, modified, err
	})
	if err != nil {
		return "", err
	}
	return target, nil
}

func (d *Destination) StreamInto(path string, modified time.Time, write func(io.Writer) error) error {
	if err := d.free(path); err != nil {
		return err
	}
	return localfile.Stream(path, modified, write)
}

func (d *Destination) Reserve(name string) (string, error) {
	if d.dest != "" {
		if err := d.free(d.dest); err != nil {
			return "", err
		}
		return d.dest, nil
	}
	if d.destDir != "" {
		if err := localfile.EnsureDir(d.destDir); err != nil {
			return "", err
		}
	}
	target := filepath.Join(d.destDir, localfile.SafeName(name))
	if d.force {
		return target, nil
	}
	return localfile.Free(target)
}

func (d *Destination) Dir() (string, error) {
	switch {
	case d.dest != "":
		dir := filepath.Dir(d.dest)
		return dir, localfile.EnsureDir(dir)
	case d.destDir != "":
		return d.destDir, localfile.EnsureDir(d.destDir)
	}
	return ".", nil
}

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
