package drive

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"time"

	pgp "github.com/ProtonMail/gopenpgp/v3/crypto"
	pgphelper "github.com/roman-16/proton-cli/internal/crypto/pgp"
	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/progress"
	"github.com/roman-16/proton-cli/internal/proton"
	"github.com/roman-16/proton-cli/internal/skip"
)

type DownloadOptions struct {
	Label            string
	Progress         progress.Sink
	OnSignatureIssue func(index int, verdict string)
}

type revisionBlock struct {
	Index        int
	BareURL      string
	Token        string
	Hash         string
	EncSignature string
}

type revisionThumbnail struct {
	Hash string
}

type revision struct {
	ManifestSignature string
	SignatureAddress  string
	SignatureEmail    string
	Thumbnails        []revisionThumbnail
	Blocks            []revisionBlock
}

func (r revision) author() string {
	if r.SignatureEmail != "" {
		return r.SignatureEmail
	}
	return r.SignatureAddress
}

func (s *Service) Download(ctx context.Context, res *Resolved, w io.Writer, opts DownloadOptions) error {
	return s.downloadFile(ctx, res.dc, res.Link, res.NodeKR, activeRevisionID(res.Link), res.Link.Size, w, opts)
}

type DownloadPlan struct {
	Top       string
	Folders   []string
	Files     []PlannedFile
	Elsewhere Elsewhere
}

type PlannedFile struct {
	Path     string
	Name     string
	Size     int64
	Plain    int64
	Modified time.Time

	link   *Link
	nodeKR *pgp.KeyRing
}

func (f PlannedFile) LinkID() string { return f.link.LinkID }

func (f PlannedFile) Revision() string { return activeRevisionID(f.link) }

type Elsewhere struct{ Documents, Spreadsheets int }

func (e Elsewhere) Count() int { return e.Documents + e.Spreadsheets }

func (e Elsewhere) With(other Elsewhere) Elsewhere {
	return Elsewhere{Documents: e.Documents + other.Documents, Spreadsheets: e.Spreadsheets + other.Spreadsheets}
}

func (e *Elsewhere) take(link *Link) bool {
	switch link.MIMEType {
	case mimeProtonDocument:
		e.Documents++
	case mimeProtonSpreadsheet:
		e.Spreadsheets++
	default:
		return false
	}
	return true
}

const (
	mimeProtonDocument    = "application/vnd.proton.doc"
	mimeProtonSpreadsheet = "application/vnd.proton.sheet"
)

func (r *Resolved) OtherProduct() (kind, app string) {
	switch r.Link.MIMEType {
	case mimeProtonDocument:
		return "Proton Docs document", "Proton Docs"
	case mimeProtonSpreadsheet:
		return "Proton Sheets spreadsheet", "Proton Sheets"
	}
	return "", ""
}

func (r *Resolved) Modified() time.Time {
	modified, _ := stamped(r.Link, r.NodeKR)
	return modified
}

func stamped(link *Link, nodeKR *pgp.KeyRing) (time.Time, int64) {
	modified := time.Unix(cmp.Or(link.RealModifyTime, link.ModifyTime), 0)
	plain := int64(-1)
	x, err := decryptXAttr(link.XAttr, nodeKR)
	if err != nil {
		return modified, plain
	}
	if x.Common.Size > 0 || len(x.Common.BlockSizes) == 0 {
		plain = x.Common.Size
	}
	if t, err := time.Parse(time.RFC3339, x.Common.ModificationTime); err == nil {
		modified = t
	}
	return modified, plain
}

func (p *DownloadPlan) Count() int { return len(p.Files) + len(p.Folders) }

func (s *Service) PlanDownload(ctx context.Context, dc *Context, path string) (*DownloadPlan, error) {
	res, err := s.ResolvePath(ctx, dc, path)
	if err != nil {
		return nil, err
	}
	if !res.IsFolder {
		return nil, errs.Problemf("%s is not a folder.", res.Describe(path))
	}
	plan := &DownloadPlan{Top: res.Describe(path)}
	if base := baseOf(path); base != "" {
		plan.Top = base
	}
	_, err = s.walkTree(ctx, dc, res.Link, res.NodeKR, "", func(f found) error {
		if f.Link.Type == protonFolder {
			plan.Folders = append(plan.Folders, f.Path)
			return nil
		}
		link := f.Link
		if plan.Elsewhere.take(&link) {
			// Recorded and not counted: the run names how many it passed over, and
			// what they hold lives in another product rather than in a revision.
			slog.DebugContext(ctx, "drive: a document of another product was passed over", "link", link.LinkID)
			return nil
		}
		nodeKR, err := unlockNode(&link, f.ParentKR, nil)
		if err != nil {
			skip.Record(ctx, skip.KindItem, f.Link.LinkID, skip.Unlockable, err)
			return nil
		}
		modified, plain := stamped(&link, nodeKR)
		plan.Files = append(plan.Files, PlannedFile{
			Path: f.Path, Name: f.label(), Size: f.Link.Size, Plain: plain, Modified: modified,
			link: &link, nodeKR: nodeKR,
		})
		return nil
	})
	return plan, err
}

func (s *Service) DownloadPlanned(ctx context.Context, dc *Context, f PlannedFile, w io.Writer, opts DownloadOptions) error {
	return s.downloadFile(ctx, dc, f.link, f.nodeKR, activeRevisionID(f.link), f.Size, w, opts)
}

func (s *Service) DownloadRevision(ctx context.Context, fr *FileRevision, w io.Writer, opts DownloadOptions) error {
	return s.downloadFile(ctx, fr.res.dc, fr.res.Link, fr.res.NodeKR, fr.ID, fr.Size, w, opts)
}

func activeRevisionID(link *Link) string {
	if link.FileProperties == nil {
		return ""
	}
	return link.FileProperties.ActiveRevision.ID
}

func (s *Service) downloadFile(ctx context.Context, dc *Context, link *Link, nodeKR *pgp.KeyRing,
	revisionID string, size int64, w io.Writer, opts DownloadOptions) error {
	if link.FileProperties == nil {
		return fmt.Errorf("%s: no file properties", link.LinkID)
	}
	kp, err := base64.StdEncoding.DecodeString(link.FileProperties.ContentKeyPacket)
	if err != nil {
		return err
	}
	sk, err := pgphelper.DecryptSessionKey(nodeKR, kp)
	if err != nil {
		return fmt.Errorf("get file session key: %w", err)
	}

	rev, err := s.revision(ctx, dc, link.LinkID, revisionID)
	if err != nil {
		return err
	}
	manifest, hashes, err := rev.manifest()
	if err != nil {
		return err
	}
	if err := s.verifyManifest(ctx, dc, nodeKR, rev.author(), manifest, rev.ManifestSignature); err != nil {
		return err
	}

	prog := progress.Of(opts.Progress)
	prog.Start(size, opts.Label)
	defer prog.Done()

	wrote := newBlockAuthor(s, dc, link.SignatureEmail, nodeKR)
	for i, b := range rev.Blocks {
		encData, err := downloadBlock(ctx, b.BareURL, b.Token)
		if err != nil {
			return fmt.Errorf("download block %d: %w", b.Index, err)
		}
		if actual := sha256.Sum256(encData); !bytes.Equal(actual[:], hashes[i]) {
			return fmt.Errorf("block %d does not match the hash the revision was signed with", b.Index)
		}
		bin, err := pgphelper.DecryptWithSessionKey(sk, encData)
		if err != nil {
			return fmt.Errorf("decrypt block %d: %w", b.Index, err)
		}
		if opts.OnSignatureIssue != nil {
			if verdict := wrote.verify(ctx, bin, b.EncSignature); verdict != "" {
				opts.OnSignatureIssue(b.Index, verdict)
			}
		}
		if _, err := w.Write(bin); err != nil {
			return err
		}
		prog.Add(int64(len(bin)))
	}
	return nil
}

type blockAuthor struct {
	s      *Service
	email  string
	nodeKR *pgp.KeyRing
	kr     *pgp.KeyRing
	loaded bool
}

func newBlockAuthor(s *Service, dc *Context, email string, nodeKR *pgp.KeyRing) *blockAuthor {
	if dc.Anonymous {
		email = ""
	}
	return &blockAuthor{s: s, email: email, nodeKR: nodeKR}
}

func (a *blockAuthor) verify(ctx context.Context, plain []byte, encSignature string) string {
	if encSignature == "" || a.email == "" {
		return ""
	}
	if !a.loaded {
		a.loaded = true
		if kr, err := a.s.addressKeyRing(ctx, a.email); err == nil {
			a.kr = kr
		}
	}
	if a.kr == nil {
		return string(pgphelper.Unverified)
	}
	msg, err := pgp.NewPGPMessageFromArmored(encSignature)
	if err != nil {
		return string(pgphelper.Invalid)
	}
	sig, err := pgphelper.Decrypt(a.nodeKR, nil, msg.Bytes(), pgp.Bytes)
	if err != nil {
		return string(pgphelper.Invalid)
	}
	if err := pgphelper.VerifyBinary(a.kr, plain, sig.Bytes(), pgp.Bytes); err != nil {
		return string(pgphelper.Classify(err))
	}
	return ""
}

func (s *Service) revision(ctx context.Context, dc *Context, linkID, revID string) (revision, error) {
	const pageSize = 50
	var out revision
	for from := 1; ; {
		q := revisionRequest(dc, linkID, revID)
		q.Query = make(map[string][]string)
		q.Query.Set("FromBlockIndex", strconv.Itoa(from))
		q.Query.Set("PageSize", strconv.Itoa(pageSize))

		var page struct{ Revision revision }
		if err := s.C.Decode(ctx, q, &page); err != nil {
			return revision{}, err
		}
		if from == 1 {
			out.ManifestSignature = page.Revision.ManifestSignature
			out.SignatureAddress = page.Revision.SignatureAddress
			out.SignatureEmail = page.Revision.SignatureEmail
			out.Thumbnails = page.Revision.Thumbnails
		}
		if len(page.Revision.Blocks) == 0 {
			return out, nil
		}
		out.Blocks = append(out.Blocks, page.Revision.Blocks...)
		last := page.Revision.Blocks[len(page.Revision.Blocks)-1].Index
		if last < from {
			return revision{}, fmt.Errorf("revision block pagination did not advance past index %d", from)
		}
		from = last + 1
	}
}

func revisionRequest(dc *Context, linkID, revID string) proton.Request {
	if dc.Public() {
		return proton.Request{
			Method: "GET",
			Path:   fmt.Sprintf("/drive/unauth/v2/volumes/%s/files/%s/revisions/%s", dc.VolumeID, linkID, revID),
		}
	}
	return proton.Request{
		Method: "GET", Path: fmt.Sprintf("/drive/shares/%s/files/%s/revisions/%s", dc.ShareID, linkID, revID),
	}
}

func (r revision) manifest() ([]byte, [][]byte, error) {
	manifest := make([]byte, 0, (len(r.Thumbnails)+len(r.Blocks))*sha256.Size)
	for i, thumbnail := range r.Thumbnails {
		hash, err := decodeSHA256(thumbnail.Hash)
		if err != nil {
			return nil, nil, fmt.Errorf("decode thumbnail %d hash: %w", i+1, err)
		}
		manifest = append(manifest, hash...)
	}
	hashes := make([][]byte, 0, len(r.Blocks))
	for i, block := range r.Blocks {
		if block.Index != i+1 {
			return nil, nil, fmt.Errorf("revision block index %d where %d was expected", block.Index, i+1)
		}
		hash, err := decodeSHA256(block.Hash)
		if err != nil {
			return nil, nil, fmt.Errorf("decode block %d hash: %w", block.Index, err)
		}
		hashes = append(hashes, hash)
		manifest = append(manifest, hash...)
	}
	return manifest, hashes, nil
}

func decodeSHA256(encoded string) ([]byte, error) {
	hash, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, err
	}
	if len(hash) != sha256.Size {
		return nil, fmt.Errorf("a hash of %d bytes, want %d", len(hash), sha256.Size)
	}
	return hash, nil
}

func (s *Service) verifyManifest(ctx context.Context, dc *Context, nodeKR *pgp.KeyRing, signedBy string, manifest []byte, signature string) error {
	if dc.Public() {
		slog.DebugContext(ctx, "drive: a public link serves no manifest signature, so the block hashes are Proton's word",
			"share", dc.Token)
		return nil
	}
	if signature == "" {
		return fmt.Errorf("the revision carries no manifest signature, so its content cannot be verified")
	}
	verificationKR := nodeKR
	if signedBy != "" {
		kr, err := s.addressKeyRing(ctx, signedBy)
		if err != nil {
			return fmt.Errorf("load the key of %s, who committed this revision: %w", signedBy, err)
		}
		verificationKR = kr
	}
	if verdict := pgphelper.VerifyBinaryStatus(verificationKR, manifest, signature); verdict != pgphelper.Verified {
		return fmt.Errorf("the revision's manifest signature is %s, so its content cannot be trusted", verdict)
	}
	return nil
}
