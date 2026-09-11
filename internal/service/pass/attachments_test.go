package pass

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"

	"github.com/roman-16/proton-cli/internal/crypto/aead"
	"github.com/roman-16/proton-cli/internal/proton"
	pb "github.com/roman-16/proton-cli/internal/service/pass/proto"
)

// sealedFile is what Proton would hand back for a file sealed here, so a test
// can open one without an account.
func sealedFile(t *testing.T, itemKey []byte, name, mimeType string, version int) itemFile {
	t.Helper()
	fileKey, err := aead.NewKey()
	if err != nil {
		t.Fatalf("NewKey: %v", err)
	}
	metadata, err := sealMetadata(fileKey, name, mimeType, version)
	if err != nil {
		t.Fatalf("sealMetadata: %v", err)
	}
	sealedKey, err := aead.Encrypt(itemKey, fileKey, []byte(aead.TagFileKey))
	if err != nil {
		t.Fatalf("seal the file key: %v", err)
	}
	return itemFile{
		FileID: "f-1", Size: 12, Metadata: metadata,
		FileKey:           base64.StdEncoding.EncodeToString(sealedKey),
		EncryptionVersion: version,
	}
}

// Both ways a file has been sealed are read, because an account holds whatever
// was written when each file was added.
func TestAFilesNameAndTypeAreReadBackAtEitherVersion(t *testing.T) {
	itemKey, err := aead.NewKey()
	if err != nil {
		t.Fatalf("NewKey: %v", err)
	}
	for _, version := range []int{1, 2} {
		file := sealedFile(t, itemKey, "passport.pdf", "application/pdf", version)
		got, err := openAttachment(itemKey, file)
		if err != nil {
			t.Fatalf("version %d: openAttachment: %v", version, err)
		}
		if got.Name != "passport.pdf" || got.MIMEType != "application/pdf" {
			t.Errorf("version %d: read back as %q (%s)", version, got.Name, got.MIMEType)
		}
		if got.version != version {
			t.Errorf("version %d: read back as version %d", version, got.version)
		}
	}
}

// A file whose version Proton leaves out is a version-1 file, which is what the
// field's absence meant before there was a second version.
func TestAFileWithNoVersionIsReadAsTheFirst(t *testing.T) {
	itemKey, err := aead.NewKey()
	if err != nil {
		t.Fatalf("NewKey: %v", err)
	}
	file := sealedFile(t, itemKey, "scan.png", "image/png", 1)
	file.EncryptionVersion = 0
	got, err := openAttachment(itemKey, file)
	if err != nil {
		t.Fatalf("openAttachment: %v", err)
	}
	if got.version != 1 {
		t.Errorf("version = %d, want 1", got.version)
	}
}

// A version-2 chunk is sealed under a tag naming its place in the file, so a
// chunk cannot be opened as though it were another one.
func TestAChunkIsSealedUnderItsOwnPlace(t *testing.T) {
	key, err := aead.NewKey()
	if err != nil {
		t.Fatalf("NewKey: %v", err)
	}
	sealed, err := aead.Encrypt(key, []byte("second piece"), []byte(chunkTag(2, 1, 3)))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if _, err := aead.Decrypt(key, sealed, []byte(chunkTag(2, 0, 3))); err == nil {
		t.Error("a chunk opened as though it were the one before it")
	}
	if _, err := aead.Decrypt(key, sealed, []byte(chunkTag(2, 1, 4))); err == nil {
		t.Error("a chunk opened as though the file were longer")
	}
	plain, err := aead.Decrypt(key, sealed, []byte(chunkTag(2, 1, 3)))
	if err != nil {
		t.Fatalf("the chunk will not open at its own place: %v", err)
	}
	if string(plain) != "second piece" {
		t.Errorf("the chunk came back as %q", plain)
	}
}

// Version 1 tags every part of a file alike, which is what makes a file written
// then readable now.
func TestTheFirstVersionTagsEveryPartAlike(t *testing.T) {
	for _, tag := range []string{metadataTag(1), chunkTag(1, 0, 1), chunkTag(1, 7, 9)} {
		if tag != aead.TagFileData {
			t.Errorf("version 1 tag = %q, want %q", tag, aead.TagFileData)
		}
	}
}

// A stored name cannot read as a path, whatever it was called where it came
// from.
func TestAStoredNameIsNotAPath(t *testing.T) {
	fileKey, err := aead.NewKey()
	if err != nil {
		t.Fatalf("NewKey: %v", err)
	}
	sealed, err := sealMetadata(fileKey, "../../etc/passwd", "text/plain", encryptionVersion)
	if err != nil {
		t.Fatalf("sealMetadata: %v", err)
	}
	raw, err := base64.StdEncoding.DecodeString(sealed)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := aead.Decrypt(fileKey, raw, []byte(metadataTag(encryptionVersion)))
	if err != nil {
		t.Fatal(err)
	}
	var meta pb.FileMetadata
	if err := proto.Unmarshal(plain, &meta); err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(meta.GetName(), `/\`) {
		t.Errorf("the stored name is a path: %q", meta.GetName())
	}
}

func TestHowManyChunksAFileGoesIn(t *testing.T) {
	cases := []struct {
		size int64
		want int
	}{
		{1, 1},
		{chunkSize, 1},
		{chunkSize + 1, 2},
		{3 * chunkSize, 3},
		{3*chunkSize + 1, 4},
	}
	for _, c := range cases {
		if got := chunkCount(c.size); got != c.want {
			t.Errorf("chunkCount(%d) = %d, want %d", c.size, got, c.want)
		}
	}
}

// How big a file is, is how big the file is. Proton counts the pieces it is
// stored as, each carrying an IV and a tag of its own.
func TestAFileIsAsBigAsItsContents(t *testing.T) {
	itemKey, err := aead.NewKey()
	if err != nil {
		t.Fatalf("NewKey: %v", err)
	}
	const contents = 4*1024*1024 + 4096
	file := sealedFile(t, itemKey, "passport.pdf", "application/pdf", encryptionVersion)
	file.Chunks = []fileChunk{{ChunkID: "c-0", Index: 0}, {ChunkID: "c-1", Index: 1}}
	file.Size = contents + int64(len(file.Chunks)*aead.Overhead)

	got, err := openAttachment(itemKey, file)
	if err != nil {
		t.Fatalf("openAttachment: %v", err)
	}
	if got.Size != contents {
		t.Errorf("size = %d, want %d", got.Size, contents)
	}
}

// The pieces go back together in the order Proton numbered them, not the order
// it happened to list them in.
func TestTheChunksAreReadInOrder(t *testing.T) {
	itemKey, err := aead.NewKey()
	if err != nil {
		t.Fatalf("NewKey: %v", err)
	}
	file := sealedFile(t, itemKey, "passport.pdf", "application/pdf", encryptionVersion)
	file.Chunks = []fileChunk{{ChunkID: "third", Index: 2}, {ChunkID: "first", Index: 0}, {ChunkID: "second", Index: 1}}

	got, err := openAttachment(itemKey, file)
	if err != nil {
		t.Fatalf("openAttachment: %v", err)
	}
	if strings.Join(got.chunks, ",") != "first,second,third" {
		t.Errorf("the chunks come in the order %v", got.chunks)
	}
}

// The identifiers a Proton account gives out, for the naming an archive does:
// the stamp a file's name carries is made of these, so a short stand-in would
// make a name the app never writes.
const (
	exampleShare = "zZ4c1dEXAMPLEshare=="
	exampleFile  = "kQ81mDx4EXAMPLEfile=="
)

// The name a file takes inside an archive is the app's own, because the archive
// is: what this writes, Proton Pass reads back.
func TestWhatAFileIsCalledInsideAnArchive(t *testing.T) {
	// The values are the app's own function's, run over the same arguments.
	cases := []struct{ name, want string }{
		{"passport.pdf", "passport.78e91fe938d5208c.pdf"},
		{"archive.tar.gz", "archive.tar.78e91fe938d5208c.gz"},
		{"receipts", "receipts.78e91fe938d5208c"},
	}
	for _, c := range cases {
		got := ExportName(exampleShare, Attachment{ID: exampleFile, Name: c.name})
		if got != c.want {
			t.Errorf("ExportName(%q) = %q, want %q", c.name, got, c.want)
		}
	}
}

// What an archive renamed, an import names back.
func TestWhatAFileIsCalledAfterAnArchive(t *testing.T) {
	cases := []struct{ entry, want string }{
		{"Proton Pass/files/passport.78e91fe938d5208c.pdf", "passport.pdf"},
		{"passport.78e91fe938d5208c.pdf", "passport.pdf"},
		{"archive.tar.78e91fe938d5208c.gz", "archive.tar.gz"},
		// A file that had no extension of its own was given the stamp as one.
		{"receipts.78e91fe938d5208c", "receipts"},
		// Anything else keeps the name it has.
		{"plain.pdf", "plain.pdf"},
	}
	for _, c := range cases {
		if got := ImportName(c.entry); got != c.want {
			t.Errorf("ImportName(%q) = %q, want %q", c.entry, got, c.want)
		}
	}
}

// A name survives the round trip through an archive, whatever it holds.
func TestANameSurvivesTheArchive(t *testing.T) {
	for _, name := range []string{"passport.pdf", "archive.tar.gz", "a.b.c.txt", "receipts"} {
		entry := ExportName(exampleShare, Attachment{ID: exampleFile, Name: name})
		if got := ImportName(entry); got != name {
			t.Errorf("%q came back as %q (via %q)", name, got, entry)
		}
	}
}

// The three refusals are made before anything is sent, so a file that would be
// turned away costs nothing to find out about.
func TestWhatAPlanWillNotTake(t *testing.T) {
	small := Upload{Name: "small.pdf", Size: 1024}
	large := Upload{Name: "large.tiff", Size: 200 << 20}
	paid := StorageLimits{Allowed: true, MaxFileSize: 100 << 20, Quota: 1 << 30}

	cases := []struct {
		what    string
		limits  StorageLimits
		uploads []Upload
		want    string
	}{
		{"a free plan", StorageLimits{}, []Upload{small}, "paid Pass plan"},
		{"a file over the limit", paid, []Upload{large}, "at most"},
		{"more than the storage left",
			StorageLimits{Allowed: true, Quota: 1 << 20, Used: 1 << 20},
			[]Upload{small}, "more room"},
		{"nothing to send at all", StorageLimits{}, nil, ""},
		{"a file the plan takes", paid, []Upload{small}, ""},
	}
	for _, c := range cases {
		err := c.limits.CheckUpload(c.uploads)
		switch {
		case c.want == "" && err != nil:
			t.Errorf("%s was refused: %v", c.what, err)
		case c.want != "" && err == nil:
			t.Errorf("%s was allowed", c.what)
		case c.want != "" && !strings.Contains(err.Error(), c.want):
			t.Errorf("%s was refused with %q, which does not say %q", c.what, err, c.want)
		}
	}
}

// chunkServer answers the chunk requests a download makes, and nothing else: a
// file's contents are all an archive needs from Proton.
type chunkServer struct{ chunks map[string][]byte }

func (c *chunkServer) Do(_ context.Context, r proton.Request) (*proton.Response, error) {
	body, ok := c.chunks[r.Path]
	if !ok {
		return nil, fmt.Errorf("no chunk canned at %s", r.Path)
	}
	return &proton.Response{Status: 200, Body: body}, nil
}

func (c *chunkServer) Decode(context.Context, proton.Request, any) error { return nil }

// sealedAttachment is a file as an item carries one, with its contents sealed
// the way Proton holds them.
func sealedAttachment(t *testing.T, shareID, itemID, name string, contents []byte) (Attachment, *chunkServer) {
	t.Helper()
	key, err := aead.NewKey()
	if err != nil {
		t.Fatalf("NewKey: %v", err)
	}
	server := &chunkServer{chunks: map[string][]byte{}}
	file := Attachment{
		ID: "f-1", Name: name, Size: int64(len(contents)),
		key: key, version: encryptionVersion,
	}
	total := chunkCount(int64(len(contents)))
	for index := range total {
		end := min((index+1)*chunkSize, len(contents))
		sealed, err := aead.Encrypt(key, contents[index*chunkSize:end],
			[]byte(chunkTag(encryptionVersion, index, total)))
		if err != nil {
			t.Fatalf("seal chunk %d: %v", index, err)
		}
		id := fmt.Sprintf("c-%d", index)
		file.chunks = append(file.chunks, id)
		server.chunks[fmt.Sprintf("/pass/v1/share/%s/item/%s/file/%s/chunk/%s",
			shareID, itemID, file.ID, id)] = sealed
	}
	return file, server
}

// An archive holds the attachments as well as the items, where the app looks for
// them and under the name the app gives them.
func TestAnArchiveCarriesTheAttachments(t *testing.T) {
	contents := bytes.Repeat([]byte("attached"), 1024)
	file, server := sealedAttachment(t, exampleShare, "i-1", "passport.pdf", contents)

	item := ExportedItem{ItemID: "i-1", ShareID: exampleShare, Files: []string{ExportName(exampleShare, file)}}
	plan := &ExportPlan{
		Doc: &ExportDocument{
			Vaults:  map[string]*ExportedVault{exampleShare: {Name: "Personal", Items: []ExportedItem{item}}},
			Version: exportVersion,
		},
		Files: []ExportFile{{ShareID: exampleShare, ItemID: "i-1", Entry: item.Files[0], File: file}},
	}

	var buf bytes.Buffer
	if err := (&Service{C: server}).WriteArchive(t.Context(), &buf, plan, "", nil); err != nil {
		t.Fatalf("WriteArchive: %v", err)
	}
	z, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("the archive will not open: %v", err)
	}
	want := "Proton Pass/files/" + item.Files[0]
	var found bool
	for _, entry := range z.File {
		if entry.Name != want {
			continue
		}
		found = true
		r, err := entry.Open()
		if err != nil {
			t.Fatalf("open %s: %v", entry.Name, err)
		}
		got, err := io.ReadAll(r)
		_ = r.Close()
		if err != nil {
			t.Fatalf("read %s: %v", entry.Name, err)
		}
		if !bytes.Equal(got, contents) {
			t.Errorf("%s holds %d bytes, want %d", entry.Name, len(got), len(contents))
		}
	}
	if !found {
		t.Fatalf("the archive holds no %s", want)
	}

	// The document is the last thing written, and each item names its own files,
	// which is how the app finds them again.
	archive := opened(t, written(t, "backup.zip", buf.Bytes()), neverAsked(t))
	back := archive.Document().Vaults[exampleShare].Items[0]
	if len(back.Files) != 1 || back.Files[0] != item.Files[0] {
		t.Errorf("the item's files came back as %v", back.Files)
	}
	up, ok := archive.Attachment(back.Files[0])
	if !ok {
		t.Fatalf("the archive does not offer %s back", back.Files[0])
	}
	if up.Name != "passport.pdf" || up.Size != int64(len(contents)) {
		t.Errorf("the file came back as %q (%d bytes)", up.Name, up.Size)
	}
}

// recordingDoer keeps the body of the last request, and answers with one item.
type recordingDoer struct{ body map[string]any }

func (d *recordingDoer) Do(context.Context, proton.Request) (*proton.Response, error) {
	return &proton.Response{Status: 200}, nil
}

func (d *recordingDoer) Decode(_ context.Context, r proton.Request, out any) error {
	d.body, _ = r.Body.(map[string]any)
	if out == nil {
		return nil
	}
	return json.Unmarshal([]byte(`{"Item":{"Revision":5}}`), out)
}

// Both lists travel on every link, empty where there is nothing to say: Proton
// takes a list of none and refuses the absence of one.
func TestLinkingSaysNothingWithAnEmptyListRatherThanNone(t *testing.T) {
	server := &recordingDoer{}
	revision, err := (&Service{C: server}).linkFiles(t.Context(), "s-1", "i-1", 4, nil, []string{"f-1"})
	if err != nil {
		t.Fatalf("linkFiles: %v", err)
	}
	if revision != 5 {
		t.Errorf("the item is at revision %d, and the link said 5", revision)
	}
	body, err := json.Marshal(server.body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"FilesToAdd":[]`) {
		t.Errorf("a link that adds nothing sends: %s", body)
	}
	if !strings.Contains(string(body), `"FilesToRemove":["f-1"]`) {
		t.Errorf("a link that removes one sends: %s", body)
	}
}
