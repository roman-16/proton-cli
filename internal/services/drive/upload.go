package drive

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"strings"

	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
	"github.com/roman-16/proton-cli/internal/api"
	"github.com/roman-16/proton-cli/internal/render"
)

// UploadOptions controls streaming uploads.
type UploadOptions struct {
	MIMEType  string
	Label     string // progress label
	Quiet     bool
	TotalHint int64 // for progress
}

// Upload streams the contents of r into a new file at destPath/name.
// destPath must be a folder. The file is created under that folder with
// Content-Type = opts.MIMEType (defaults to application/octet-stream).
func (s *Service) Upload(ctx context.Context, dc *Context, destPath, name string, r io.Reader, opts UploadOptions) error {
	if opts.MIMEType == "" {
		opts.MIMEType = "application/octet-stream"
	}
	parent, err := s.ResolvePath(ctx, dc, destPath)
	if err != nil {
		return fmt.Errorf("target folder not found: %w", err)
	}
	if !parent.IsFolder {
		return fmt.Errorf("%s is not a folder", destPath)
	}
	parentLink, err := s.getLink(ctx, parent.ShareID, parent.LinkID)
	if err != nil {
		return err
	}
	hk, err := hashKeyOf(parentLink, parent.NodeKR)
	if err != nil {
		return err
	}
	hash, err := lookupHash(strings.ToLower(name), hk)
	if err != nil {
		return err
	}
	encName, err := encryptName(name, parent.NodeKR, dc.AddrKR)
	if err != nil {
		return err
	}
	nodeKey, nodePass, nodePassSig, nodePriv, err := genNodeKeys(parent.NodeKR, dc.AddrKR)
	if err != nil {
		return err
	}
	nodeKR, err := pgp.NewKeyRing(nodePriv)
	if err != nil {
		return err
	}
	sessionKey, contentKP, contentKPSig, err := genFileKeys(nodeKR, dc.AddrKR)
	if err != nil {
		return err
	}

	var createResult struct {
		Code int
		File struct{ ID, RevisionID string }
	}
	if err := s.C.Send(ctx, api.Request{
		Method: "POST", Path: "/drive/shares/" + parent.ShareID + "/files",
		Body: map[string]any{
			"Name": encName, "Hash": hash,
			"ParentLinkID":   parent.LinkID,
			"NodePassphrase": nodePass, "NodePassphraseSignature": nodePassSig,
			"SignatureAddress":          dc.AddrEmail,
			"NodeKey":                   nodeKey,
			"MIMEType":                  opts.MIMEType,
			"ContentKeyPacket":          contentKP,
			"ContentKeyPacketSignature": contentKPSig,
		},
	}, &createResult); err != nil {
		return err
	}
	linkID := createResult.File.ID
	revisionID := createResult.File.RevisionID

	var verResult struct {
		VerificationCode string
		ContentKeyPacket string
	}
	if err := s.C.Send(ctx, api.Request{
		Method: "GET", Path: fmt.Sprintf("/drive/shares/%s/links/%s/revisions/%s/verification", parent.ShareID, linkID, revisionID),
	}, &verResult); err != nil {
		return fmt.Errorf("get verification data: %w", err)
	}
	verCode, err := base64.StdEncoding.DecodeString(verResult.VerificationCode)
	if err != nil {
		return fmt.Errorf("decode verification: %w", err)
	}

	progress := &render.Progress{Total: opts.TotalHint, Label: opts.Label, Quiet: opts.Quiet}
	progress.Start()
	defer progress.Finish()

	type blockInfo struct {
		Index    int
		Hash     string
		EncSig   string
		Size     int
		EncData  []byte
		Verifier string
	}
	const blockSize = 4 * 1024 * 1024
	buf := make([]byte, blockSize)
	var blocks []blockInfo
	index := 0
	for {
		n, err := io.ReadFull(r, buf)
		if err == io.EOF {
			break
		}
		if err != nil && err != io.ErrUnexpectedEOF {
			return err
		}
		chunk := make([]byte, n)
		copy(chunk, buf[:n])
		index++
		enc, encSig, errE := encryptBlock(chunk, sessionKey, nodeKR, dc.AddrKR)
		if errE != nil {
			return fmt.Errorf("encrypt block %d: %w", index, errE)
		}
		h := sha256.Sum256(enc)
		verTok := make([]byte, len(verCode))
		for j := range verCode {
			if j < len(enc) {
				verTok[j] = verCode[j] ^ enc[j]
			} else {
				verTok[j] = verCode[j]
			}
		}
		blocks = append(blocks, blockInfo{
			Index: index, Hash: base64.StdEncoding.EncodeToString(h[:]),
			EncSig: encSig, Size: len(enc), EncData: enc,
			Verifier: base64.StdEncoding.EncodeToString(verTok),
		})
		if err == io.ErrUnexpectedEOF || n < blockSize {
			break
		}
	}

	blockList := make([]map[string]any, len(blocks))
	for i, b := range blocks {
		blockList[i] = map[string]any{
			"Hash": b.Hash, "EncSignature": b.EncSig, "Size": b.Size, "Index": b.Index,
			"Verifier": map[string]string{"Token": b.Verifier},
		}
	}
	var uploadResult struct {
		Code        int
		UploadLinks []struct{ Token, BareURL string }
	}
	if err := s.C.Send(ctx, api.Request{
		Method: "POST", Path: "/drive/blocks",
		Body: map[string]any{
			"AddressID": dc.AddrID, "ShareID": parent.ShareID,
			"LinkID": linkID, "RevisionID": revisionID, "BlockList": blockList,
		},
	}, &uploadResult); err != nil {
		return err
	}

	for i, link := range uploadResult.UploadLinks {
		boundary := "proton-cli-boundary"
		var body strings.Builder
		body.WriteString("--" + boundary + "\r\n")
		body.WriteString("Content-Disposition: form-data; name=\"Block\"; filename=\"blob\"\r\n")
		body.WriteString("Content-Type: application/octet-stream\r\n\r\n")
		body.Write(blocks[i].EncData)
		body.WriteString("\r\n--" + boundary + "--\r\n")
		req, err := http.NewRequestWithContext(ctx, "POST", link.BareURL, strings.NewReader(body.String()))
		if err != nil {
			return err
		}
		req.Header.Set("pm-storage-token", link.Token)
		req.Header.Set("Content-Type", "multipart/form-data; boundary="+boundary)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return fmt.Errorf("upload block %d: %w", i+1, err)
		}
		respBody, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode >= 400 {
			return fmt.Errorf("upload block %d: HTTP %d: %s", i+1, resp.StatusCode, string(respBody))
		}
		progress.Add(int64(blocks[i].Size))
	}

	var manifestBytes []byte
	for _, b := range blocks {
		h, _ := base64.StdEncoding.DecodeString(b.Hash)
		manifestBytes = append(manifestBytes, h...)
	}
	sig, err := dc.AddrKR.SignDetached(pgp.NewPlainMessage(manifestBytes))
	if err != nil {
		return err
	}
	manifestSig, err := sig.GetArmored()
	if err != nil {
		return err
	}
	blockTokens := make([]map[string]any, len(uploadResult.UploadLinks))
	for i, link := range uploadResult.UploadLinks {
		blockTokens[i] = map[string]any{"Index": blocks[i].Index, "Token": link.Token}
	}
	return s.C.Send(ctx, api.Request{
		Method: "PUT", Path: fmt.Sprintf("/drive/shares/%s/files/%s/revisions/%s", parent.ShareID, linkID, revisionID),
		Body: map[string]any{
			"BlockList": blockTokens, "State": 1,
			"ManifestSignature": manifestSig, "SignatureAddress": dc.AddrEmail,
		},
	}, nil)
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
