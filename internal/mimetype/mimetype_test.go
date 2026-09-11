package mimetype

import "testing"

func TestByName(t *testing.T) {
	cases := []struct{ name, want string }{
		{"report.pdf", "application/pdf"},
		{"REPORT.PDF", "application/pdf"},
		{"photo.png", "image/png"},
		// Proton names this one itself; the general table calls .ts a video
		// stream, and a TypeScript file uploaded from an editor is not one.
		{"main.ts", "application/typescript"},
		{"script.py", "text/x-python"},
		// No general table carries the camera formats.
		{"DSC01234.arw", "image/x-sony-arw"},
		{"IMG_0001.CR3", "image/x-canon-cr3"},
		{"notes", Unknown},
		{"archive.zzz", Unknown},
	}
	for _, c := range cases {
		if got := ByName(c.name); got != c.want {
			t.Errorf("ByName(%q) = %q, want %q", c.name, got, c.want)
		}
	}
}

// A type is stored beside the bytes, so it never carries the parameters a
// lookup table hangs on one.
func TestByNameCarriesNoParameters(t *testing.T) {
	for _, name := range []string{"page.html", "style.css", "data.json"} {
		if got := ByName(name); got != bare(got) {
			t.Errorf("ByName(%q) = %q, which carries parameters", name, got)
		}
	}
}

func TestByContent(t *testing.T) {
	pdf := append([]byte("%PDF-1.7\n"), make([]byte, 32)...)
	png := []byte("\x89PNG\r\n\x1a\n")
	cases := []struct {
		what string
		head []byte
		name string
		want string
	}{
		// What it is beats what it is called.
		{"a PDF called nothing in particular", pdf, "scan", "application/pdf"},
		{"a PDF misnamed as a text file", pdf, "scan.txt", "application/pdf"},
		{"a PNG", png, "image", "image/png"},
		// Text tells the sniffer nothing, so the name decides.
		{"a JSON document", []byte(`{"a":1}`), "rows.json", "application/json"},
		{"text with nothing to go on", []byte("hello"), "notes", "text/plain"},
		// Neither has anything to say.
		{"unrecognised bytes", []byte{0x00, 0x01, 0x02, 0x03}, "blob", Unknown},
	}
	for _, c := range cases {
		if got := ByContent(c.head, c.name); got != c.want {
			t.Errorf("ByContent(%s) = %q, want %q", c.what, got, c.want)
		}
	}
}

// Nothing is read past the head, so an empty file still gets a type.
func TestByContentOfNothing(t *testing.T) {
	if got := ByContent(nil, "report.pdf"); got != "application/pdf" {
		t.Errorf("ByContent(nil, report.pdf) = %q, want application/pdf", got)
	}
}
