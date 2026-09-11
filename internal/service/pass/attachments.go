package pass

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"mime/multipart"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode/utf16"

	"google.golang.org/protobuf/proto"

	"github.com/roman-16/proton-cli/internal/crypto/aead"
	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/mimetype"
	"github.com/roman-16/proton-cli/internal/progress"
	"github.com/roman-16/proton-cli/internal/proton"
	"github.com/roman-16/proton-cli/internal/ref"
	pb "github.com/roman-16/proton-cli/internal/service/pass/proto"
	"github.com/roman-16/proton-cli/internal/skip"
	"github.com/roman-16/proton-cli/internal/units"
)

// Files attached to an item.
//
// A file is sealed under a key of its own, that key under the item's, and the
// contents travel in chunks that are sealed one at a time - so neither this nor
// Proton ever holds a whole attachment in the clear. The file belongs to the
// item the way a field does: adding or removing one writes a new revision, and
// the history keeps what was taken away.

const (
	// chunkSize is how much of a file goes in one piece.
	chunkSize = 4 * 1024 * 1024
	// encryptionVersion is what a file written now says about how it is sealed:
	// its metadata under a tag of its own, and each chunk under a tag naming that
	// chunk's place. Version 1, which tags the lot alike, is still read.
	encryptionVersion = 2
	// sniffSize is how much of a file is looked at to decide what it is.
	sniffSize = 8192

	// What one link request may carry, which is Proton's limit rather than ours.
	maxFilesToAdd    = 10
	maxFilesToRemove = 100
)

// hasFiles is the item flag that says an item carries files, so reading one
// that carries none costs no request to find that out.
const hasFiles = 1 << 3

// Attachment is one file on an item.
type Attachment struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	MIMEType   string `json:"mime_type,omitempty"`
	Size       int64  `json:"size"`
	CreateTime int64  `json:"create_time,omitempty"`

	// key opens the contents, chunks are the pieces in the order they go back
	// together, and version says how both are sealed.
	key     []byte
	chunks  []string
	version int
	// uid is the identity Proton keeps across a removal and a restore, which is
	// what tells a file that came back from a second copy of it.
	uid string
	// removed is the item revision this file was taken off at, and zero for one
	// that is still on the item.
	removed int
}

// RemovedAt is the item revision this file was taken off at, which a listing of
// what can be restored says beside each one.
func (a Attachment) RemovedAt() int { return a.removed }

// Attachments are the files an item carries now.
func (s *Service) Attachments(ctx context.Context, shareID, itemID string) ([]Attachment, error) {
	files, err := s.itemFiles(ctx, func(q url.Values) proton.Request {
		return proton.Request{
			Method: "GET",
			Path:   fmt.Sprintf("/pass/v1/share/%s/item/%s/files", shareID, itemID),
			Query:  q,
		}
	})
	if err != nil {
		return nil, err
	}
	return s.openAttachments(ctx, shareID, itemID, files)
}

// RemovedAttachments are the files taken off an item and not put back, which is
// everything there is to restore.
func (s *Service) RemovedAttachments(ctx context.Context, shareID, itemID string) ([]Attachment, error) {
	all, err := s.everyAttachment(ctx, shareID, itemID)
	if err != nil {
		return nil, err
	}
	// A restored file is a second row with the same identity as the one that was
	// removed, so what is on the item now is what says a removal was undone.
	present := make(map[string]bool, len(all))
	for _, a := range all {
		if a.removed == 0 {
			present[a.uid] = true
		}
	}
	out := make([]Attachment, 0, len(all))
	for _, a := range all {
		if a.removed != 0 && !present[a.uid] {
			out = append(out, a)
		}
	}
	return out, nil
}

// everyAttachment is every file the item has ever carried, the removed ones
// included.
func (s *Service) everyAttachment(ctx context.Context, shareID, itemID string) ([]Attachment, error) {
	files, err := s.itemFiles(ctx, func(q url.Values) proton.Request {
		return proton.Request{
			Method: "GET",
			Path:   fmt.Sprintf("/pass/v1/share/%s/item/%s/revisions/files", shareID, itemID),
			Query:  q,
		}
	})
	if err != nil {
		return nil, err
	}
	return s.openAttachments(ctx, shareID, itemID, files)
}

// itemFile is one file as Proton describes it, still sealed.
type itemFile struct {
	FileID            string
	Size              int64
	Metadata          string
	FileKey           string
	ItemKeyRotation   int
	EncryptionVersion int
	RevisionAdded     int
	RevisionRemoved   *int
	PersistentFileUID string
	CreateTime        int64
	Chunks            []fileChunk
}

// fileChunk is one piece of a file's contents, with the place it goes back in.
type fileChunk struct {
	ChunkID string
	Index   int
	Size    int64
}

// itemFiles reads a listing of files page by page, whichever of the two the
// caller asked for: the files on the item now, or every file it has ever had.
//
// Proton pages this by the ID the previous answer ended with, so an item with
// more files than a page holds still has all of them read.
func (s *Service) itemFiles(ctx context.Context, page func(url.Values) proton.Request) ([]itemFile, error) {
	var since string
	var seen int
	return proton.All(ctx, func(ctx context.Context, _ int) ([]itemFile, bool, error) {
		q := proton.Query()
		if since != "" {
			q.Set("Since", since)
		}
		var r struct {
			Files struct {
				Files  []itemFile
				Total  int
				LastID string
			}
		}
		if err := s.C.Decode(ctx, page(q), &r); err != nil {
			return nil, false, err
		}
		// Proton names an ending point on the last page as readily as on any other,
		// so the count is what says there is nothing after this.
		since = r.Files.LastID
		seen += len(r.Files.Files)
		return r.Files.Files, since != "" && len(r.Files.Files) > 0 && seen < r.Files.Total, nil
	})
}

// openAttachments unseals what a listing described.
//
// A file's name and type are sealed under the file's own key, and that key under
// the item key of the rotation it names - so every rotation the item has had is
// opened once and used for whichever files were sealed under it.
func (s *Service) openAttachments(ctx context.Context, shareID, itemID string, files []itemFile) ([]Attachment, error) {
	if len(files) == 0 {
		return nil, nil
	}
	keys, err := s.itemKeys(ctx, shareID, itemID)
	if err != nil {
		return nil, err
	}
	out := make([]Attachment, 0, len(files))
	for _, f := range files {
		itemKey, ok := keys[f.ItemKeyRotation]
		if !ok {
			skip.Record(ctx, skip.KindAttachment, f.FileID, skip.NoKey, nil)
			continue
		}
		a, err := openAttachment(itemKey, f)
		if err != nil {
			skip.Record(ctx, skip.KindAttachment, f.FileID, skip.Undecryptable, err)
			continue
		}
		out = append(out, *a)
	}
	return out, nil
}

// openAttachment unseals one file's key and what it says about itself.
func openAttachment(itemKey []byte, f itemFile) (*Attachment, error) {
	sealedKey, err := base64.StdEncoding.DecodeString(f.FileKey)
	if err != nil {
		return nil, err
	}
	fileKey, err := aead.Decrypt(itemKey, sealedKey, []byte(aead.TagFileKey))
	if err != nil {
		return nil, err
	}
	sealed, err := base64.StdEncoding.DecodeString(f.Metadata)
	if err != nil {
		return nil, err
	}
	version := f.EncryptionVersion
	if version == 0 {
		version = 1
	}
	plain, err := aead.Decrypt(fileKey, sealed, []byte(metadataTag(version)))
	if err != nil {
		return nil, err
	}
	var meta pb.FileMetadata
	if err := proto.Unmarshal(plain, &meta); err != nil {
		return nil, err
	}
	// The pieces go back together in the order Proton numbered them, which is
	// not necessarily the order it listed them in.
	chunks := slices.Clone(f.Chunks)
	slices.SortFunc(chunks, func(a, b fileChunk) int { return a.Index - b.Index })
	// Proton counts the sealed pieces, each of which carries an IV and a tag that
	// are not part of the file. A person means the file, and that is the number a
	// download writes out.
	out := &Attachment{
		ID: f.FileID, Name: meta.GetName(), MIMEType: meta.GetMimeType(),
		Size: f.Size - int64(len(chunks)*aead.Overhead), CreateTime: f.CreateTime,
		key: fileKey, version: version, uid: f.PersistentFileUID,
	}
	for _, c := range chunks {
		out.chunks = append(out.chunks, c.ChunkID)
	}
	if f.RevisionRemoved != nil {
		out.removed = *f.RevisionRemoved
	}
	return out, nil
}

// metadataTag is what a file's name and type are sealed under, which the file
// says the version of.
func metadataTag(version int) string {
	if version == 1 {
		return aead.TagFileData
	}
	return aead.TagFileMetadata
}

// chunkTag is what one piece of the contents is sealed under.
func chunkTag(version, index, total int) string {
	if version == 1 {
		return aead.TagFileData
	}
	return aead.TagFileChunk(index, total)
}

// ── naming one ──

// ResolveAttachment finds the file on an item that a reference names, by ID or
// by name.
func (s *Service) ResolveAttachment(ctx context.Context, shareID, itemID, reference string) (*Attachment, error) {
	files, err := s.Attachments(ctx, shareID, itemID)
	if err != nil {
		return nil, err
	}
	return pickAttachment(reference, files, func(a Attachment) string {
		return fmt.Sprintf("%s (%s)", a.Name, units.Size(a.Size))
	})
}

// ResolveRemovedAttachment finds a file that was taken off the item and has not
// been put back, which is the only kind there is anything to restore.
func (s *Service) ResolveRemovedAttachment(ctx context.Context, shareID, itemID, reference string) (*Attachment, error) {
	gone, err := s.RemovedAttachments(ctx, shareID, itemID)
	if err != nil {
		return nil, err
	}
	return pickAttachment(reference, gone, func(a Attachment) string {
		return fmt.Sprintf("%s (%s), removed at revision %d", a.Name, units.Size(a.Size), a.removed)
	})
}

// pickAttachment matches a reference against a set of files: the ID outright,
// or the name regardless of case.
func pickAttachment(reference string, files []Attachment, label func(Attachment) string) (*Attachment, error) {
	for _, a := range files {
		if a.ID == reference {
			return &a, nil
		}
	}
	matches := make([]Attachment, 0, 1)
	for _, a := range files {
		if strings.EqualFold(a.Name, reference) {
			matches = append(matches, a)
		}
	}
	got, err := ref.Pick("attachment", reference, matches,
		func(a Attachment) string { return a.ID }, label)
	if err != nil {
		return nil, err
	}
	return &got, nil
}

// ── reading one back ──

// AttachmentDownload writes one file's contents out, a chunk at a time.
func (s *Service) AttachmentDownload(ctx context.Context, shareID, itemID string, a Attachment, w io.Writer, sink progress.Sink) error {
	report := progress.Of(sink)
	report.Start(a.Size, "Downloading "+a.Name)
	defer report.Done()
	for i, chunkID := range a.chunks {
		resp, err := s.C.Do(ctx, proton.Request{
			Method: "GET",
			Path: fmt.Sprintf("/pass/v1/share/%s/item/%s/file/%s/chunk/%s",
				shareID, itemID, a.ID, chunkID),
		})
		if err != nil {
			return err
		}
		plain, err := aead.Decrypt(a.key, resp.Body, []byte(chunkTag(a.version, i, len(a.chunks))))
		if err != nil {
			return fmt.Errorf("chunk %d of %s will not open: %w", i+1, a.Name, err)
		}
		if _, err := w.Write(plain); err != nil {
			return err
		}
		report.Add(int64(len(plain)))
	}
	return nil
}

// ── changing one ──

// AttachmentRename changes what a file is called, leaving the item alone: a name
// is the file's own, so writing one is not an edit of the item it hangs on.
func (s *Service) AttachmentRename(ctx context.Context, shareID, itemID string, a Attachment, name string) error {
	sealed, err := sealMetadata(a.key, name, a.MIMEType, a.version)
	if err != nil {
		return err
	}
	return s.C.Decode(ctx, proton.Request{
		Method: "PUT",
		Path:   fmt.Sprintf("/pass/v1/share/%s/item/%s/file/%s/metadata", shareID, itemID, a.ID),
		Body:   map[string]any{"Metadata": sealed},
	}, nil)
}

// AttachmentRestore puts a removed file back on the item.
func (s *Service) AttachmentRestore(ctx context.Context, shareID, itemID string, a Attachment) error {
	key, rotation, err := s.currentItemKey(ctx, shareID, itemID)
	if err != nil {
		return err
	}
	sealed, err := aead.Encrypt(key, a.key, []byte(aead.TagFileKey))
	if err != nil {
		return err
	}
	return s.C.Decode(ctx, proton.Request{
		Method: "POST",
		Path:   fmt.Sprintf("/pass/v1/share/%s/item/%s/file/%s/restore", shareID, itemID, a.ID),
		Body: map[string]any{
			"FileKey":         base64.StdEncoding.EncodeToString(sealed),
			"ItemKeyRotation": rotation,
		},
	}, nil)
}

// currentItemKey opens the item's newest key, which is the one anything written
// now is sealed under.
func (s *Service) currentItemKey(ctx context.Context, shareID, itemID string) ([]byte, int, error) {
	sk, err := s.decryptShareKeys(ctx, shareID)
	if err != nil {
		return nil, 0, err
	}
	return s.latestItemKey(ctx, sk, shareID, itemID)
}

// ── putting one there ──

// Upload is one file on its way to an item: what it will be called, how big it
// is, and how to read it.
//
// It is an opener rather than a path because a file headed for an item is as
// often inside a backup as on the disk, and neither is read twice or read whole.
type Upload struct {
	Name string
	Size int64
	Open func() (io.ReadCloser, error)
	// Progress receives byte counts; nil discards them.
	Progress progress.Sink
}

// pendingFile is an uploaded file that hangs on nothing yet: Proton holds the
// contents, and the key stays here until there is an item to seal it under.
type pendingFile struct {
	id  string
	key []byte
}

// uploadPending sends a file's contents. Linking is what puts it on an item.
func (s *Service) uploadPending(ctx context.Context, up Upload) (pendingFile, error) {
	source, err := up.Open()
	if err != nil {
		return pendingFile{}, err
	}
	defer func() { _ = source.Close() }()

	// What a file is, is decided from its first bytes, and those bytes are then
	// put back in front of the rest: a file inside an archive is a stream that
	// cannot be rewound.
	head := make([]byte, sniffSize)
	n, err := io.ReadFull(source, head)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return pendingFile{}, err
	}
	head = head[:n]
	contents := io.MultiReader(bytes.NewReader(head), source)

	fileKey, err := aead.NewKey()
	if err != nil {
		return pendingFile{}, err
	}
	sealed, err := sealMetadata(fileKey, up.Name, mimetype.ByContent(head, up.Name), encryptionVersion)
	if err != nil {
		return pendingFile{}, err
	}
	total := chunkCount(up.Size)
	var created struct{ File struct{ FileID string } }
	if err := s.C.Decode(ctx, proton.Request{
		Method: "POST", Path: "/pass/v1/file",
		Body: map[string]any{
			"Metadata":          sealed,
			"ChunkCount":        total,
			"EncryptionVersion": encryptionVersion,
		},
	}, &created); err != nil {
		return pendingFile{}, err
	}

	report := progress.Of(up.Progress)
	report.Start(up.Size, "Uploading "+up.Name)
	defer report.Done()
	buf := make([]byte, chunkSize)
	for index := range total {
		n, err := io.ReadFull(contents, buf)
		if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
			return pendingFile{}, err
		}
		ct, err := aead.Encrypt(fileKey, buf[:n], []byte(chunkTag(encryptionVersion, index, total)))
		if err != nil {
			return pendingFile{}, err
		}
		if err := s.uploadChunk(ctx, created.File.FileID, index, ct); err != nil {
			return pendingFile{}, err
		}
		report.Add(int64(n))
	}
	return pendingFile{id: created.File.FileID, key: fileKey}, nil
}

// chunkCount is how many pieces a file of that size goes in. An empty file is
// refused before this, so every file has at least one.
func chunkCount(size int64) int {
	if size <= 0 {
		return 1
	}
	return int((size + chunkSize - 1) / chunkSize)
}

// uploadChunk sends one sealed piece as the form Proton takes.
func (s *Service) uploadChunk(ctx context.Context, fileID string, index int, data []byte) error {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	if err := w.WriteField("ChunkIndex", strconv.Itoa(index)); err != nil {
		return err
	}
	part, err := w.CreateFormFile("ChunkData", "blob")
	if err != nil {
		return err
	}
	if _, err := part.Write(data); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return s.C.Decode(ctx, proton.Request{
		Method: "POST", Path: "/pass/v1/file/" + fileID + "/chunk",
		Body: buf.Bytes(), ContentType: w.FormDataContentType(),
	}, nil)
}

// sealMetadata seals what a file says about itself under the file's own key.
func sealMetadata(fileKey []byte, name, mimeType string, version int) (string, error) {
	encoded, err := proto.Marshal(&pb.FileMetadata{Name: sanitizeFileName(name), MimeType: mimeType})
	if err != nil {
		return "", err
	}
	ct, err := aead.Encrypt(fileKey, encoded, []byte(metadataTag(version)))
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(ct), nil
}

// sanitizeFileName keeps a stored name from reading as a path.
func sanitizeFileName(name string) string {
	return strings.TrimSpace(strings.NewReplacer("/", "_", `\`, "_").Replace(name))
}

// linkFiles puts files on an item and takes files off it, in the batches Proton
// accepts, and answers with the revision the item ends up at.
func (s *Service) linkFiles(ctx context.Context, shareID, itemID string, revision int, add []pendingFile, remove []string) (int, error) {
	if len(add) == 0 && len(remove) == 0 {
		return revision, nil
	}
	// Only a file being put on the item needs a key to seal it under; taking one
	// off says nothing about keys.
	var key []byte
	if len(add) > 0 {
		var err error
		if key, _, err = s.currentItemKey(ctx, shareID, itemID); err != nil {
			return 0, err
		}
	}
	adds := slices.Collect(slices.Chunk(add, maxFilesToAdd))
	removes := slices.Collect(slices.Chunk(remove, maxFilesToRemove))
	for i := range max(len(adds), len(removes)) {
		// Both lists travel on every call, empty where there is nothing to say:
		// Proton takes a list of none and refuses the absence of one.
		toAdd := make([]map[string]any, 0, maxFilesToAdd)
		if i < len(adds) {
			for _, pending := range adds[i] {
				sealed, err := aead.Encrypt(key, pending.key, []byte(aead.TagFileKey))
				if err != nil {
					return 0, err
				}
				toAdd = append(toAdd, map[string]any{
					"FileID":  pending.id,
					"FileKey": base64.StdEncoding.EncodeToString(sealed),
				})
			}
		}
		toRemove := []string{}
		if i < len(removes) {
			toRemove = removes[i]
		}
		var r struct{ Item struct{ Revision int } }
		if err := s.C.Decode(ctx, proton.Request{
			Method: "POST", Path: fmt.Sprintf("/pass/v1/share/%s/item/%s/link_files", shareID, itemID),
			Body: map[string]any{
				"ItemRevision":  revision,
				"FilesToAdd":    toAdd,
				"FilesToRemove": toRemove,
			},
		}, &r); err != nil {
			return 0, err
		}
		revision = r.Item.Revision
	}
	return revision, nil
}

// ── what a plan allows ──

// StorageLimits is what Proton says this account may do with attachments.
type StorageLimits struct {
	// Allowed reports whether the plan carries file storage at all.
	Allowed bool
	// MaxFileSize is the largest one file may be, and Used and Quota are how much
	// of the account's storage is spoken for.
	MaxFileSize int64
	Used        int64
	Quota       int64
}

// Free is how much more may be stored.
func (l StorageLimits) Free() int64 { return max(l.Quota-l.Used, 0) }

// StorageLimits reads what the account's plan allows.
//
// It is asked before anything is sent, because every one of the three ways an
// upload is refused is knowable in advance, and finding out at the end means
// having spent the transfer to learn it.
func (s *Service) StorageLimits(ctx context.Context) (*StorageLimits, error) {
	var r struct {
		Access struct {
			Plan struct {
				StorageAllowed     bool
				StorageMaxFileSize int64
				StorageUsed        int64
				StorageQuota       int64
			}
		}
	}
	if err := s.C.Decode(ctx, proton.Request{
		Method: "GET", Path: "/pass/v1/user/access", Reads: true,
	}, &r); err != nil {
		return nil, err
	}
	plan := r.Access.Plan
	return &StorageLimits{
		Allowed:     plan.StorageAllowed,
		MaxFileSize: plan.StorageMaxFileSize,
		Used:        plan.StorageUsed,
		Quota:       plan.StorageQuota,
	}, nil
}

// ── a file inside a backup ──

// ExportName is what a file is called inside an archive.
//
// Two items may hold files of the same name, and an archive is flat, so the name
// carries where the file came from. It is the app's own naming, because the
// archive is the app's: what this writes, Proton Pass reads back.
func ExportName(shareID string, a Attachment) string {
	base, ext := fileParts(a.Name)
	return fmt.Sprintf("%s.%s%s%s", base, hashCode(shareID), hashCode(a.ID), ext)
}

// ImportName recovers the name a file had before an archive renamed it.
func ImportName(path string) string {
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

// ── judging a file before it is sent ──

// CheckUpload refuses a file the plan will not take, before any of it is sent.
//
// The three refusals are Proton's own: an account without file storage, a file
// over the size one may be, and more than the storage that is left.
func (l StorageLimits) CheckUpload(uploads []Upload) error {
	if len(uploads) == 0 {
		return nil
	}
	if !l.Allowed {
		return errs.Problemf("Attachments need a paid Pass plan.")
	}
	var total int64
	for _, up := range uploads {
		if l.MaxFileSize > 0 && up.Size > l.MaxFileSize {
			return errs.Problemf("%s is %s; an attachment may be at most %s.",
				up.Name, units.Size(up.Size), units.Size(l.MaxFileSize))
		}
		total += up.Size
	}
	if l.Quota > 0 && total > l.Free() {
		return errs.Problemf("%s of attachments needs more room than the %s left in your Pass storage.",
			units.Size(total), units.Size(l.Free()))
	}
	return nil
}

// ReadUpload describes a local file --attach named, and refuses one there is
// nothing to send. Nothing here needs an account, so a mistyped path is refused
// before anything is signed in to.
func ReadUpload(path string) (Upload, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Upload{}, errs.Problemf("--attach %s: no such file.", path)
		}
		return Upload{}, errs.Problemf("--attach %s: %v.", path, err)
	}
	if info.IsDir() {
		return Upload{}, errs.Problemf("--attach %s is a directory.", path).
			Hint("attach the files inside it")
	}
	if info.Size() == 0 {
		return Upload{}, errs.Problemf("--attach %s is empty.", path)
	}
	return Upload{
		Name: filepath.Base(path),
		Size: info.Size(),
		Open: func() (io.ReadCloser, error) { return os.Open(path) },
	}, nil
}
