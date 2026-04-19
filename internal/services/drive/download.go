package drive

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"

	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
	"github.com/roman-16/proton-cli/internal/api"
	"github.com/roman-16/proton-cli/internal/render"
)

// DownloadOptions controls streaming downloads.
type DownloadOptions struct {
	Label string
	Quiet bool
}

// Download streams a decrypted file to w.
func (s *Service) Download(ctx context.Context, dc *Context, path string, w io.Writer, opts DownloadOptions) error {
	res, err := s.ResolvePath(ctx, dc, path)
	if err != nil {
		return err
	}
	if res.IsFolder {
		return fmt.Errorf("%s is a folder, not a file", path)
	}
	link, err := s.getLink(ctx, res.ShareID, res.LinkID)
	if err != nil {
		return err
	}
	if link.FileProperties == nil {
		return fmt.Errorf("%s: no file properties", path)
	}
	kp, err := base64.StdEncoding.DecodeString(link.FileProperties.ContentKeyPacket)
	if err != nil {
		return err
	}
	sk, err := res.NodeKR.DecryptSessionKey(kp)
	if err != nil {
		return fmt.Errorf("get file session key: %w", err)
	}

	var rev struct {
		Revision struct {
			Blocks []struct {
				Index        int
				BareURL      string
				Token        string
				EncSignature string
			}
		}
	}
	q := api.Request{
		Method: "GET",
		Path:   fmt.Sprintf("/drive/shares/%s/files/%s/revisions/%s", res.ShareID, res.LinkID, link.FileProperties.ActiveRevision.ID),
	}
	q.Query = make(map[string][]string)
	q.Query.Set("FromBlockIndex", "1")
	q.Query.Set("PageSize", "50")
	if err := s.C.Send(ctx, q, &rev); err != nil {
		return err
	}

	progress := &render.Progress{Total: link.Size, Label: opts.Label, Quiet: opts.Quiet}
	progress.Start()
	defer progress.Finish()

	for i, b := range rev.Revision.Blocks {
		req, err := http.NewRequestWithContext(ctx, "GET", b.BareURL, nil)
		if err != nil {
			return err
		}
		req.Header.Set("pm-storage-token", b.Token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return fmt.Errorf("download block %d: %w", i+1, err)
		}
		encData, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			return err
		}
		dec, err := sk.Decrypt(encData)
		if err != nil {
			return fmt.Errorf("decrypt block %d: %w", i+1, err)
		}
		bin := dec.GetBinary()
		if _, err := w.Write(bin); err != nil {
			return err
		}
		progress.Add(int64(len(bin)))
	}
	_ = pgp.GetUnixTime
	return nil
}
