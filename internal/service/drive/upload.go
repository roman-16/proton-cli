package drive

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"sync"
	"time"

	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
	"github.com/roman-16/proton-cli/internal/mimetype"
	"github.com/roman-16/proton-cli/internal/progress"
	"github.com/roman-16/proton-cli/internal/proton"
)

type UploadOptions struct {
	MIMEType string
	// Modified is when the file was last changed where it came from, which the
	// revision records so every other client shows it. A stream has none.
	Modified time.Time
	// Label names the transfer for the progress report.
	Label string
	// Progress receives byte counts; nil discards them.
	Progress  progress.Sink
	TotalHint int64
	// Photo, when set, marks the committed revision as a photo (added to the
	// commit body verbatim, e.g. {MainPhotoLinkID, CaptureTime, ContentHash}).
	Photo map[string]any
}

// encBlock is one 4 MiB file chunk after encryption, carrying everything the
// block-link request, the parallel upload, a token refresh, and the final
// revision manifest need. data is released once the block is uploaded.
type encBlock struct {
	index    int
	hash     string // base64(sha256(data))
	rawHash  []byte // sha256(data), concatenated into the manifest
	encSig   string
	verifier string // base64 verifier token
	size     int
	data     []byte
}

func (b *encBlock) listEntry() map[string]any {
	return map[string]any{
		"Hash": b.hash, "EncSignature": b.encSig, "Size": b.size, "Index": b.index,
		"Verifier": map[string]string{"Token": b.verifier},
	}
}

// Upload writes r into Drive as the file the plan describes.
//
// The plan says where the bytes go and under what name, and whether they become
// a new file or a new revision of the one already there. Deciding that before
// any of this runs is what lets the command report - and a dry run promise - the
// same thing that happens.
func (s *Service) Upload(ctx context.Context, dc *Context, plan *UploadPlan, r io.Reader, opts UploadOptions) error {
	if plan.Nothing {
		return nil
	}
	if opts.MIMEType == "" {
		// A file is typed by its name here, as it is in Drive's own client, so
		// that what other clients show of it is what it is.
		opts.MIMEType = mimetype.ByName(plan.Name)
	}

	by, err := s.author(ctx, dc)
	if err != nil {
		return err
	}
	linkID, revisionID, sessionKey, nodeKR, err := s.startRevision(ctx, dc, plan, by, opts.MIMEType)
	if err != nil {
		return err
	}

	var verResult struct {
		VerificationCode string
		ContentKeyPacket string
	}
	if err := s.C.Decode(ctx, verificationRequest(dc, linkID, revisionID), &verResult); err != nil {
		return fmt.Errorf("get verification data: %w", err)
	}
	verCode, err := base64.StdEncoding.DecodeString(verResult.VerificationCode)
	if err != nil {
		return fmt.Errorf("decode verification: %w", err)
	}

	prog := progress.Of(opts.Progress)
	prog.Start(opts.TotalHint, opts.Label)
	defer prog.Done()

	up, err := s.streamBlocks(ctx, dc, linkID, revisionID, by, sessionKey, nodeKR, verCode, r, prog)
	if err != nil {
		return err
	}

	manifestBytes, err := buildManifest(up.hashes)
	if err != nil {
		return err
	}
	sig, err := by.key(nodeKR).SignDetached(pgp.NewPlainMessage(manifestBytes))
	if err != nil {
		return err
	}
	manifestSig, err := sig.GetArmored()
	if err != nil {
		return err
	}
	xattr, err := encryptXAttr(up.record(opts.Modified), nodeKR, by.key(nodeKR))
	if err != nil {
		return err
	}
	commit := map[string]any{"ManifestSignature": manifestSig, "XAttr": xattr}
	by.attribute(commit, dc)
	if opts.Photo != nil {
		commit["Photo"] = opts.Photo
	}
	return s.C.Decode(ctx, commitRequest(dc, linkID, revisionID, commit), nil)
}

// verificationRequest asks for what a block's verifier token is built from, of
// whichever endpoint serves the tree the file is in.
func verificationRequest(dc *Context, linkID, revisionID string) proton.Request {
	if dc.Public() {
		return proton.Request{
			Method: "GET",
			Path:   fmt.Sprintf("/drive/urls/%s/links/%s/revisions/%s/verification", dc.Token, linkID, revisionID),
		}
	}
	return proton.Request{
		Method: "GET",
		Path:   fmt.Sprintf("/drive/shares/%s/links/%s/revisions/%s/verification", dc.ShareID, linkID, revisionID),
	}
}

// commitRequest finishes a revision, of whichever endpoint serves the tree the
// file is in.
//
// What the commit carries is the manifest signature, the file's own record of
// itself, and who wrote both. The blocks are Proton's to have collected as they
// arrived: it knows which of them landed under the revision, and its own clients
// tell it nothing about them here.
func commitRequest(dc *Context, linkID, revisionID string, body map[string]any) proton.Request {
	if dc.Public() {
		return proton.Request{
			Method: "PUT", Body: body,
			Path: fmt.Sprintf("/drive/urls/%s/files/%s/revisions/%s", dc.Token, linkID, revisionID),
		}
	}
	return proton.Request{
		Method: "PUT", Body: body,
		Path: fmt.Sprintf("/drive/shares/%s/files/%s/revisions/%s", dc.ShareID, linkID, revisionID),
	}
}

// uploaded is what the bytes turned out to be once every block has landed: the
// hashes the revision's manifest is built over, and the file's account of
// itself.
type uploaded struct {
	// hashes is the SHA-256 of each encrypted block, by index.
	hashes map[int][]byte
	// blockSizes is how many bytes each block held before encryption, in order,
	// and size how many there were altogether.
	blockSizes []int
	size       int64
	sha1       string
}

// record is the file's own account of itself, for the revision to carry.
func (u *uploaded) record(modified time.Time) xAttr {
	var x xAttr
	if !modified.IsZero() {
		x.Common.ModificationTime = modified.UTC().Format(xAttrTime)
	}
	x.Common.Size = u.size
	x.Common.BlockSizes = u.blockSizes
	x.Common.Digests.SHA1 = u.sha1
	return x
}

// streamBlocks reads r in 4 MiB chunks, encrypts each, requests upload links in
// batches, and uploads blocks in parallel with per-block retry. Memory stays
// bounded to the encryption window plus in-flight uploads (never the whole
// file).
//
// What it reads is described as it goes: the plain bytes are digested and
// counted where they are read, which is the one place they pass through in
// order, and the reading is done by the time anything reads that back.
func (s *Service) streamBlocks(
	ctx context.Context, dc *Context, linkID, revisionID string, by author,
	sessionKey *pgp.SessionKey, nodeKR *pgp.KeyRing,
	verCode []byte, r io.Reader, prog progress.Sink,
) (*uploaded, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	signKR := by.key(nodeKR)
	plain := &uploaded{hashes: map[int][]byte{}}
	digest := sha1.New() //nolint:gosec // Proton records the content digest as SHA-1
	var (
		mu       sync.Mutex
		firstErr error
	)
	setErr := func(e error) {
		mu.Lock()
		if firstErr == nil {
			firstErr = e
			cancel()
		}
		mu.Unlock()
	}

	// Producer: read -> encrypt -> hash one block at a time, bounded by the
	// channel so the whole file is never resident in memory.
	encCh := make(chan *encBlock, uploadBufferBlocks)
	go func() {
		defer close(encCh)
		buf := make([]byte, driveBlockSize)
		index := 0
		for {
			if ctx.Err() != nil {
				return
			}
			n, rerr := io.ReadFull(r, buf)
			if rerr == io.EOF {
				return
			}
			if rerr != nil && rerr != io.ErrUnexpectedEOF {
				setErr(rerr)
				return
			}
			index++
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			digest.Write(chunk)
			plain.blockSizes = append(plain.blockSizes, n)
			plain.size += int64(n)
			enc, encSig, eerr := encryptBlock(chunk, sessionKey, nodeKR, signKR)
			if eerr != nil {
				setErr(fmt.Errorf("encrypt block %d: %w", index, eerr))
				return
			}
			sum := sha256.Sum256(enc)
			blk := &encBlock{
				index:    index,
				hash:     base64.StdEncoding.EncodeToString(sum[:]),
				rawHash:  append([]byte(nil), sum[:]...),
				encSig:   encSig,
				verifier: base64.StdEncoding.EncodeToString(xorVerifier(verCode, enc)),
				size:     len(enc),
				data:     enc,
			}
			select {
			case encCh <- blk:
			case <-ctx.Done():
				return
			}
			if rerr == io.ErrUnexpectedEOF || n < driveBlockSize {
				return
			}
		}
	}()

	// Uploaders: a bounded worker pool draining the encryption window.
	sem := make(chan struct{}, uploadParallelJobs)
	var wg sync.WaitGroup
	dispatch := func(blk *encBlock, link uploadLink) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			if ctx.Err() != nil {
				return
			}
			refresh := func(rctx context.Context) (uploadLink, error) {
				links, err := s.requestBlockLinks(rctx, dc, linkID, revisionID, by, []*encBlock{blk})
				if err != nil {
					return uploadLink{}, err
				}
				return links[0], nil
			}
			if err := uploadBlock(ctx, blk.index, blk.data, link, refresh); err != nil {
				setErr(err)
				return
			}
			mu.Lock()
			plain.hashes[blk.index] = blk.rawHash
			prog.Add(int64(blk.size))
			mu.Unlock()
			blk.data = nil // release the encrypted payload once uploaded
		}()
	}

	// Dispatcher: batch encrypted blocks, request their links, hand them to the
	// worker pool (blocking on the pool bounds total in-flight memory).
	flush := func(batch []*encBlock) error {
		if len(batch) == 0 {
			return nil
		}
		links, err := s.requestBlockLinks(ctx, dc, linkID, revisionID, by, batch)
		if err != nil {
			return err
		}
		for i, blk := range batch {
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return ctx.Err()
			}
			dispatch(blk, links[i])
		}
		return nil
	}

	batch := make([]*encBlock, 0, uploadLinkBatch)
	for done := false; !done; {
		select {
		case blk, ok := <-encCh:
			if !ok {
				if err := flush(batch); err != nil {
					setErr(err)
				}
				done = true
				break
			}
			batch = append(batch, blk)
			if len(batch) >= uploadLinkBatch {
				if err := flush(batch); err != nil {
					setErr(err)
					done = true
					break
				}
				batch = make([]*encBlock, 0, uploadLinkBatch)
			}
		case <-ctx.Done():
			done = true
		}
	}

	wg.Wait()

	mu.Lock()
	err := firstErr
	mu.Unlock()
	if err != nil {
		return nil, err
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	plain.sha1 = hex.EncodeToString(digest.Sum(nil))
	return plain, nil
}

// buildManifest assembles what a revision is committed over: the hash of every
// block, concatenated in index order.
//
// A hash is recorded once its block has landed, so a gap is a block that did
// not: it errors rather than signing a manifest that describes a different file
// from the one Proton holds, which also asserts the parallel uploads produced a
// complete 1..N set.
func buildManifest(rawHashByIdx map[int][]byte) ([]byte, error) {
	var manifest []byte
	for idx := 1; idx <= len(rawHashByIdx); idx++ {
		h, ok := rawHashByIdx[idx]
		if !ok {
			return nil, fmt.Errorf("missing hash for block %d", idx)
		}
		manifest = append(manifest, h...)
	}
	return manifest, nil
}

// xorVerifier builds a block's verifier token: verCode XOR the block's leading
// bytes, zero-padded to verCode's length.
func xorVerifier(verCode, enc []byte) []byte {
	out := make([]byte, len(verCode))
	for j := range verCode {
		if j < len(enc) {
			out[j] = verCode[j] ^ enc[j]
		} else {
			out[j] = verCode[j]
		}
	}
	return out
}

func genFileKeys(nodeKR *pgp.KeyRing) (*pgp.SessionKey, string, string, error) {
	sk, err := pgp.GenerateSessionKey()
	if err != nil {
		return nil, "", "", err
	}
	kp, err := nodeKR.EncryptSessionKey(sk)
	if err != nil {
		return nil, "", "", err
	}
	sig, err := nodeKR.SignDetached(pgp.NewPlainMessage(sk.Key))
	if err != nil {
		return nil, "", "", err
	}
	armoredSig, err := sig.GetArmored()
	if err != nil {
		return nil, "", "", err
	}
	return sk, base64.StdEncoding.EncodeToString(kp), armoredSig, nil
}

func encryptBlock(data []byte, sk *pgp.SessionKey, nodeKR, signKR *pgp.KeyRing) ([]byte, string, error) {
	msg := pgp.NewPlainMessage(data)
	enc, err := sk.Encrypt(msg)
	if err != nil {
		return nil, "", err
	}
	sig, err := signKR.SignDetached(msg)
	if err != nil {
		return nil, "", err
	}
	sigMsg := pgp.NewPlainMessage(sig.GetBinary())
	encSig, err := nodeKR.Encrypt(sigMsg, nil)
	if err != nil {
		return nil, "", err
	}
	armSig, err := encSig.GetArmored()
	if err != nil {
		return nil, "", err
	}
	return enc, armSig, nil
}
