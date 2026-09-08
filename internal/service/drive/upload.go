package drive

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"sync"

	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
	"github.com/roman-16/proton-cli/internal/progress"
	"github.com/roman-16/proton-cli/internal/proton"
)

type UploadOptions struct {
	MIMEType string
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
		opts.MIMEType = "application/octet-stream"
	}
	parent := plan.parent

	linkID, revisionID, sessionKey, nodeKR, err := s.startRevision(ctx, dc, plan, opts.MIMEType)
	if err != nil {
		return err
	}

	var verResult struct {
		VerificationCode string
		ContentKeyPacket string
	}
	if err := s.C.Decode(ctx, proton.Request{
		Method: "GET", Path: fmt.Sprintf("/drive/shares/%s/links/%s/revisions/%s/verification", parent.dc.ShareID, linkID, revisionID),
	}, &verResult); err != nil {
		return fmt.Errorf("get verification data: %w", err)
	}
	verCode, err := base64.StdEncoding.DecodeString(verResult.VerificationCode)
	if err != nil {
		return fmt.Errorf("decode verification: %w", err)
	}

	prog := progress.Of(opts.Progress)
	prog.Start(opts.TotalHint, opts.Label)
	defer prog.Done()

	rawHashByIdx, tokenByIdx, err := s.streamBlocks(
		ctx, parent.dc.ShareID, linkID, revisionID, dc.AddrID,
		sessionKey, nodeKR, dc.AddrKR, verCode, r, prog,
	)

	if err != nil {
		return err
	}

	manifestBytes, blockTokens, err := buildRevisionCommit(rawHashByIdx, tokenByIdx)
	if err != nil {
		return err
	}
	sig, err := dc.AddrKR.SignDetached(pgp.NewPlainMessage(manifestBytes))
	if err != nil {
		return err
	}
	manifestSig, err := sig.GetArmored()
	if err != nil {
		return err
	}
	commit := map[string]any{
		"BlockList": blockTokens, "State": 1,
		"ManifestSignature": manifestSig, "SignatureAddress": dc.AddrEmail,
	}
	if opts.Photo != nil {
		commit["Photo"] = opts.Photo
	}
	return s.C.Decode(ctx, proton.Request{
		Method: "PUT", Path: fmt.Sprintf("/drive/shares/%s/files/%s/revisions/%s", parent.dc.ShareID, linkID, revisionID),
		Body: commit,
	}, nil)
}

// streamBlocks reads r in 4 MiB chunks, encrypts each, requests upload links in
// batches, and uploads blocks in parallel with per-block retry. Memory stays
// bounded to the encryption window plus in-flight uploads (never the whole
// file). It returns, per block index, the raw block hash (for the manifest) and
// the token the successful upload used (for the revision BlockList).
func (s *Service) streamBlocks(
	ctx context.Context,
	shareID, linkID, revisionID, addrID string,
	sessionKey *pgp.SessionKey, nodeKR, addrKR *pgp.KeyRing,
	verCode []byte, r io.Reader, prog progress.Sink,
) (map[int][]byte, map[int]string, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		mu           sync.Mutex
		firstErr     error
		rawHashByIdx = map[int][]byte{}
		tokenByIdx   = map[int]string{}
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
			enc, encSig, eerr := encryptBlock(chunk, sessionKey, nodeKR, addrKR)
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
				links, err := s.requestBlockLinks(rctx, shareID, linkID, revisionID, addrID, []*encBlock{blk})
				if err != nil {
					return uploadLink{}, err
				}
				return links[0], nil
			}
			tok, err := uploadBlock(ctx, blk.index, blk.data, link, refresh)
			if err != nil {
				setErr(err)
				return
			}
			mu.Lock()
			tokenByIdx[blk.index] = tok
			rawHashByIdx[blk.index] = blk.rawHash
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
		links, err := s.requestBlockLinks(ctx, shareID, linkID, revisionID, addrID, batch)
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
		return nil, nil, err
	}
	if ctx.Err() != nil {
		return nil, nil, ctx.Err()
	}
	return rawHashByIdx, tokenByIdx, nil
}

// buildRevisionCommit assembles the manifest (block hashes concatenated in
// index order) and the revision BlockList (index + token) from the per-block
// results. It errors on any gap so a revision is never committed with a missing
// block, which also asserts the parallel uploads produced a complete 1..N set.
func buildRevisionCommit(rawHashByIdx map[int][]byte, tokenByIdx map[int]string) ([]byte, []map[string]any, error) {
	n := len(tokenByIdx)
	var manifest []byte
	blockList := make([]map[string]any, 0, n)
	for idx := 1; idx <= n; idx++ {
		h, ok := rawHashByIdx[idx]
		if !ok {
			return nil, nil, fmt.Errorf("missing hash for block %d", idx)
		}
		tok, ok := tokenByIdx[idx]
		if !ok {
			return nil, nil, fmt.Errorf("missing token for block %d", idx)
		}
		manifest = append(manifest, h...)
		blockList = append(blockList, map[string]any{"Index": idx, "Token": tok})
	}
	return manifest, blockList, nil
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

func genFileKeys(nodeKR, addrKR *pgp.KeyRing) (*pgp.SessionKey, string, string, error) {
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

func encryptBlock(data []byte, sk *pgp.SessionKey, nodeKR, addrKR *pgp.KeyRing) ([]byte, string, error) {
	msg := pgp.NewPlainMessage(data)
	enc, err := sk.Encrypt(msg)
	if err != nil {
		return nil, "", err
	}
	if addrKR == nil {
		return enc, "", nil
	}
	sig, err := addrKR.SignDetached(msg)
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
