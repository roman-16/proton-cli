package drive

import "testing"

// Which files the index holds the text of is the whole cost of the second
// pass: every yes is a download, and every wrong yes is a download of
// something a keyword could never match.
func TestWhichFilesAreText(t *testing.T) {
	for _, tc := range []struct {
		name     string
		mimeType string
		file     string
		want     bool
	}{
		{name: "plain text", mimeType: "text/plain", file: "notes.txt", want: true},
		{name: "markdown", mimeType: "text/markdown", file: "notes.md", want: true},
		{name: "a comma-separated table", mimeType: "text/csv", file: "rows.csv", want: true},
		{name: "a page", mimeType: "text/html", file: "page.html", want: true},
		{name: "source", mimeType: "application/typescript", file: "app.ts", want: true},
		{name: "a shell script", mimeType: "application/x-sh", file: "deploy.sh", want: true},
		{name: "settings", mimeType: "application/json", file: "config.json", want: true},
		{name: "a drawing, whose markup is not what it is", mimeType: "image/svg+xml", file: "chart.svg"},
		{name: "a photograph", mimeType: "image/jpeg", file: "holiday.jpg"},
		{name: "a document nothing here parses", mimeType: "application/pdf", file: "invoice.pdf"},
		{name: "an archive", mimeType: "application/zip", file: "backup.zip"},
		{
			name: "a type nobody stored, read off the name instead",
			file: "vienna-parking.md", want: true,
		},
		{
			name:     "a type stored as the word for anything at all",
			mimeType: "application/octet-stream", file: "notes.txt", want: true,
		},
		{name: "a name that says nothing either", mimeType: "", file: "dump"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := isText(tc.mimeType, tc.file); got != tc.want {
				t.Errorf("isText(%q, %q) = %v, want %v", tc.mimeType, tc.file, got, tc.want)
			}
		})
	}
}

// The type is what the uploader's client made of the name, so the bytes are
// asked as well: a keyword that matched the innards of a photograph would be
// matching noise, and one that matched a tag would be matching markup.
func TestWhatAFilesBytesAreWorthSearching(t *testing.T) {
	for _, tc := range []struct {
		name     string
		mimeType string
		file     string
		data     []byte
		want     string
		text     bool
	}{
		{
			name: "text is itself", mimeType: "text/plain", file: "notes.txt",
			data: []byte("vienna parking permit"), want: "vienna parking permit", text: true,
		},
		{
			name: "a page is what it reads as", mimeType: "text/html", file: "page.html",
			data: []byte("<p>vienna <b>parking</b></p>"), want: "vienna parking", text: true,
		},
		{
			name: "a page named rather than typed", file: "page.html",
			data: []byte("<p>vienna</p>"), want: "vienna", text: true,
		},
		{
			name: "bytes that are not text at all", mimeType: "text/plain", file: "photo.txt",
			data: []byte{0xff, 0xfe, 0x00, 0x01},
		},
		{
			name: "text with a hole punched through it", mimeType: "text/plain", file: "notes.txt",
			data: []byte("parking\x00permit"),
		},
		{
			name: "accents survive", mimeType: "text/plain", file: "notes.txt",
			data: []byte("Jürgen"), want: "Jürgen", text: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := asText(tc.mimeType, tc.file, tc.data)
			if ok != tc.text {
				t.Fatalf("asText read it as text = %v, want %v", ok, tc.text)
			}
			if got != tc.want {
				t.Errorf("asText = %q, want %q", got, tc.want)
			}
		})
	}
}
