package drive

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"strconv"

	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
	pgphelper "github.com/roman-16/proton-cli/internal/crypto/pgp"
	"github.com/roman-16/proton-cli/internal/progress"
	"github.com/roman-16/proton-cli/internal/proton"
)

// A download is not finished when the bytes arrive; it is finished when the bytes
// are known to be the ones that were uploaded.
//
// Proton commits a revision by signing a manifest: the SHA-256 of every encrypted
// block, concatenated in order. So the integrity of a whole file reduces to one
// signature over a list of hashes, and the order of operations is what makes that
// worth having. The manifest is verified before a single block is fetched, which
// makes the hash list trustworthy; then each block is checked against its hash
// before it is decrypted and written. Verifying afterwards, once the bytes have
// gone to a file or down a pipe, would only be able to report the bad news.

type DownloadOptions struct {
	// Label names the transfer for the progress report.
	Label string
	// Progress receives byte counts; nil discards them.
	Progress progress.Sink
	// OnSignatureIssue reports a block whose author signature does not check out.
	//
	// It is a report rather than a failure because it answers a different question
	// from the manifest: the manifest says the content is what was committed, which
	// is either true or not, while a per-block signature says who wrote it, which
	// this machine may simply not hold the key to judge. Blocks uploaded
	// anonymously carry no such signature at all.
	OnSignatureIssue func(index int, verdict string)
}

// revisionBlock is one encrypted block of a revision.
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

// revision is the metadata a download needs: the blocks, the thumbnails that come
// before them in the manifest, and who signed it.
type revision struct {
	ManifestSignature string
	SignatureAddress  string
	SignatureEmail    string
	Thumbnails        []revisionThumbnail
	Blocks            []revisionBlock
}

// author is the address whose key signed the manifest. Older revisions name it in
// SignatureAddress; newer ones in SignatureEmail.
func (r revision) author() string {
	if r.SignatureEmail != "" {
		return r.SignatureEmail
	}
	return r.SignatureAddress
}

// Download streams and decrypts what a file holds now.
//
// It takes the resolved file rather than a path, because the command already has
// one: the name the bytes land under on disk is the item's, which is not a thing
// a path always carries - the root of a shared file is `/`.
func (s *Service) Download(ctx context.Context, res *Resolved, w io.Writer, opts DownloadOptions) error {
	return s.downloadFile(ctx, res.ShareID, res.Link, res.NodeKR, activeRevisionID(res.Link), res.Link.Size, w, opts)
}

// DownloadRevision streams and decrypts one earlier version of a file.
//
// Reading an old version is a read: the file keeps whatever it holds now, which
// is the difference between wanting the bytes back and wanting them back in
// place.
func (s *Service) DownloadRevision(ctx context.Context, fr *FileRevision, w io.Writer, opts DownloadOptions) error {
	return s.downloadFile(ctx, fr.res.ShareID, fr.res.Link, fr.res.NodeKR, fr.ID, fr.Size, w, opts)
}

// activeRevisionID is the version a file holds now, which is what a download
// means when nobody names another. A link with no file properties has none, and
// downloadFile is where that is reported.
func activeRevisionID(link *Link) string {
	if link.FileProperties == nil {
		return ""
	}
	return link.FileProperties.ActiveRevision.ID
}

// downloadFile streams and decrypts one revision of a file link whose node key
// ring (nodeKR) has already been unwrapped. The content session key belongs to
// the file rather than to any one revision, so every version opens with it.
func (s *Service) downloadFile(ctx context.Context, shareID string, link *Link, nodeKR *pgp.KeyRing,
	revisionID string, size int64, w io.Writer, opts DownloadOptions) error {
	if link.FileProperties == nil {
		return fmt.Errorf("%s: no file properties", link.LinkID)
	}
	kp, err := base64.StdEncoding.DecodeString(link.FileProperties.ContentKeyPacket)
	if err != nil {
		return err
	}
	sk, err := nodeKR.DecryptSessionKey(kp)
	if err != nil {
		return fmt.Errorf("get file session key: %w", err)
	}

	rev, err := s.revision(ctx, shareID, link.LinkID, revisionID)
	if err != nil {
		return err
	}
	manifest, hashes, err := rev.manifest()
	if err != nil {
		return err
	}
	if err := s.verifyManifest(ctx, nodeKR, rev.author(), manifest, rev.ManifestSignature); err != nil {
		return err
	}

	prog := progress.Of(opts.Progress)
	prog.Start(size, opts.Label)
	defer prog.Done()

	author := newBlockAuthor(s, link.SignatureEmail, nodeKR)
	for i, b := range rev.Blocks {
		encData, err := downloadBlock(ctx, b.BareURL, b.Token)
		if err != nil {
			return fmt.Errorf("download block %d: %w", b.Index, err)
		}
		if actual := sha256.Sum256(encData); !bytes.Equal(actual[:], hashes[i]) {
			return fmt.Errorf("block %d does not match the hash the revision was signed with", b.Index)
		}
		dec, err := sk.Decrypt(encData)
		if err != nil {
			return fmt.Errorf("decrypt block %d: %w", b.Index, err)
		}
		if opts.OnSignatureIssue != nil {
			if verdict := author.verify(ctx, dec, b.EncSignature); verdict != "" {
				opts.OnSignatureIssue(b.Index, verdict)
			}
		}
		bin := dec.GetBinary()
		if _, err := w.Write(bin); err != nil {
			return err
		}
		prog.Add(int64(len(bin)))
	}
	return nil
}

// blockAuthor checks the per-block signature that says who wrote a block.
//
// The signature is stored encrypted to the node key and covers the block's
// plaintext, so opening it needs the node and judging it needs the uploader's
// address key. That key is fetched once and only if a block turns out to carry a
// signature at all.
type blockAuthor struct {
	s      *Service
	email  string
	nodeKR *pgp.KeyRing
	kr     *pgp.KeyRing
	loaded bool
}

func newBlockAuthor(s *Service, email string, nodeKR *pgp.KeyRing) *blockAuthor {
	return &blockAuthor{s: s, email: email, nodeKR: nodeKR}
}

// verify returns "" when there is nothing to report, and the verdict otherwise.
func (a *blockAuthor) verify(ctx context.Context, plain *pgp.PlainMessage, encSignature string) string {
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
	sig, err := a.nodeKR.Decrypt(msg, nil, pgp.GetUnixTime())
	if err != nil {
		return string(pgphelper.Invalid)
	}
	if err := a.kr.VerifyDetached(plain, pgp.NewPGPSignature(sig.GetBinary()), pgp.GetUnixTime()); err != nil {
		return string(pgphelper.Classify(err))
	}
	return ""
}

// revision reads every page of a revision's block list.
func (s *Service) revision(ctx context.Context, shareID, linkID, revID string) (revision, error) {
	const pageSize = 50
	var out revision
	for from := 1; ; {
		q := proton.Request{
			Method: "GET",
			Path:   fmt.Sprintf("/drive/shares/%s/files/%s/revisions/%s", shareID, linkID, revID),
		}
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

// manifest rebuilds what the revision's signature covers: every thumbnail hash
// followed by every block hash, in order. It returns the block hashes separately so
// each block can be checked as it arrives.
//
// The block indexes have to run unbroken from 1, because a manifest assembled from
// a list with a gap in it is a manifest over different content than the file being
// written.
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

// verifyManifest checks the revision signature against the key of whoever
// committed it, or against the node key for content uploaded anonymously.
func (s *Service) verifyManifest(ctx context.Context, nodeKR *pgp.KeyRing, author string, manifest []byte, signature string) error {
	if signature == "" {
		return fmt.Errorf("the revision carries no manifest signature, so its content cannot be verified")
	}
	verificationKR := nodeKR
	if author != "" {
		kr, err := s.addressKeyRing(ctx, author)
		if err != nil {
			return fmt.Errorf("load the key of %s, who committed this revision: %w", author, err)
		}
		verificationKR = kr
	}
	if verdict := pgphelper.VerifyDetachedStatus(verificationKR, pgp.NewPlainMessage(manifest), signature); verdict != pgphelper.Verified {
		return fmt.Errorf("the revision's manifest signature is %s, so its content cannot be trusted", verdict)
	}
	return nil
}
