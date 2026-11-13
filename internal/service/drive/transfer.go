package drive

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/roman-16/proton-cli/internal/proton"
)

// Block-transfer tuning, mirroring the Proton web client's upload worker
// (WebClients packages/drive-store/store/_uploads/constants.ts) so large files
// stream with bounded memory and survive transient failures:
//
//   - blocks are 4 MiB (FILE_CHUNK_SIZE);
//   - only a small window of encrypted blocks is held in memory at once
//     (uploadBufferBlocks + uploadParallelJobs), never the whole file;
//   - upload links are requested in batches as the window drains;
//   - blocks upload in parallel (uploadParallelJobs ~ MAX_UPLOAD_JOBS);
//   - each transfer retries transient failures (blockMaxRetries ~
//     MAX_RETRIES_BEFORE_FAIL), honours 429 Retry-After (bounded by
//     blockMaxRateLimit ~ MAX_TOO_MANY_REQUESTS_WAIT), and refreshes an
//     expired storage token (blockTokenTTL ~ TOKEN_EXPIRATION_TIME).
const (
	driveBlockSize     = 4 * 1024 * 1024
	storedBlockLimit   = driveBlockSize + 1024*1024
	uploadBufferBlocks = 15
	uploadLinkBatch    = 10
	uploadParallelJobs = 5
	blockMaxRetries    = 3
	blockTransferQuery = 90 * time.Second
	blockTokenTTL      = 3 * time.Hour
	blockMaxRateLimit  = time.Hour
)

var blockRetryBaseDelay = 500 * time.Millisecond

var blockClient = &http.Client{CheckRedirect: proton.SameOrigin}

// storageError is what a block transfer failed with, said without quoting what
// failed.
//
// A signed storage URL is a credential for the block it names, and it appears in
// every transport error net/http produces and in some of what an endpoint
// answers with. An error is read in logs, pasted into issues and printed by
// whatever ran the command, so what it carries is the operation and the status
// and nothing else. The cause is kept for errors.Is, which is how a cancelled
// context stays recognisable as one.
type storageError struct {
	operation string
	status    int
	cause     error
}

func (e *storageError) Error() string {
	if e.status != 0 {
		return fmt.Sprintf("%s: storage service returned HTTP %d", e.operation, e.status)
	}
	return e.operation + ": storage request failed"
}

func (e *storageError) Unwrap() error { return e.cause }

func storageFailed(operation string, status int, cause error) error {
	return &proton.NetworkError{Err: &storageError{operation: operation, status: status, cause: cause}}
}

type uploadLink struct {
	Token   string
	BareURL string
	created time.Time
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func backoffDelay(attempt int) time.Duration {
	d := blockRetryBaseDelay << attempt
	if d <= 0 || d > 30*time.Second {
		return 30 * time.Second
	}
	return d
}

func retryAfter(header string, attempt int) time.Duration {
	if secs, err := strconv.Atoi(strings.TrimSpace(header)); err == nil && secs >= 0 {
		d := time.Duration(secs) * time.Second
		if d > blockMaxRateLimit {
			d = blockMaxRateLimit
		}
		return d
	}
	return backoffDelay(attempt)
}

// tokenRejected reports whether a storage response status means the upload
// token is no longer usable, so a fresh link must be requested (mirrors the web
// client's NOT_FOUND / ALREADY_EXISTS handling).
func tokenRejected(status int) bool {
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound,
		http.StatusConflict, http.StatusGone, http.StatusUnprocessableEntity:
		return true
	}
	return false
}

// putBlock uploads one encrypted block's bytes to its storage URL and returns
// the HTTP status and the Retry-After header. The response body is discarded:
// storage can reflect the signed URL back, and no caller needs its text.
func putBlock(ctx context.Context, link uploadLink, data []byte) (status int, retryAfterHdr string, err error) {
	const boundary = "proton-cli-boundary"
	var buf bytes.Buffer
	buf.WriteString("--" + boundary + "\r\n")
	buf.WriteString("Content-Disposition: form-data; name=\"Block\"; filename=\"blob\"\r\n")
	buf.WriteString("Content-Type: application/octet-stream\r\n\r\n")
	buf.Write(data)
	buf.WriteString("\r\n--" + boundary + "--\r\n")

	req, err := http.NewRequestWithContext(ctx, "POST", link.BareURL, bytes.NewReader(buf.Bytes()))
	if err != nil {
		return 0, "", storageFailed("upload", 0, err)
	}
	req.Header.Set("pm-storage-token", link.Token)
	req.Header.Set("Content-Type", "multipart/form-data; boundary="+boundary)
	resp, err := blockClient.Do(req)
	if err != nil {
		return 0, "", storageFailed("upload", 0, err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	return resp.StatusCode, resp.Header.Get("Retry-After"), nil
}

func uploadBlock(ctx context.Context, index int, data []byte, link uploadLink, refresh func(context.Context) (uploadLink, error)) error {
	for attempt := 0; ; attempt++ {
		if time.Since(link.created) > blockTokenTTL {
			fresh, err := refresh(ctx)
			if err != nil {
				return err
			}
			link = fresh
		}

		attemptCtx, cancel := context.WithTimeout(ctx, blockTransferQuery)
		status, ra, err := putBlock(attemptCtx, link, data)
		cancel()

		if err == nil && status >= 200 && status < 300 {
			return nil
		}
		if attempt >= blockMaxRetries {
			if err != nil {
				return fmt.Errorf("upload block %d: %w", index, err)
			}
			return storageFailed(fmt.Sprintf("upload block %d", index), status, nil)
		}

		switch {
		case err != nil: // network / timeout
			if werr := sleepCtx(ctx, backoffDelay(attempt)); werr != nil {
				return werr
			}
		case status == http.StatusTooManyRequests:
			if werr := sleepCtx(ctx, retryAfter(ra, attempt)); werr != nil {
				return werr
			}
		case tokenRejected(status): // expired / already-committed token: get a fresh link
			fresh, rerr := refresh(ctx)
			if rerr != nil {
				return rerr
			}
			link = fresh
		case status >= 500:
			if werr := sleepCtx(ctx, backoffDelay(attempt)); werr != nil {
				return werr
			}
		default: // other 4xx: not recoverable
			return storageFailed(fmt.Sprintf("upload block %d", index), status, nil)
		}
	}
}

func getBlock(ctx context.Context, url, token string) (data []byte, status int, retryAfterHdr string, err error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, 0, "", storageFailed("download", 0, err)
	}
	req.Header.Set("pm-storage-token", token)
	resp, err := blockClient.Do(req)
	if err != nil {
		return nil, 0, "", storageFailed("download", 0, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return nil, resp.StatusCode, resp.Header.Get("Retry-After"), nil
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, storedBlockLimit+1))
	if err != nil {
		return nil, resp.StatusCode, "", storageFailed("download", 0, err)
	}
	if len(b) > storedBlockLimit {
		return nil, resp.StatusCode, "", errBlockTooLarge
	}
	return b, resp.StatusCode, "", nil
}

var errBlockTooLarge = storageFailed("download block", 0, errors.New("the block exceeds the size a block can be"))

func downloadBlock(ctx context.Context, url, token string) ([]byte, error) {
	for attempt := 0; ; attempt++ {
		attemptCtx, cancel := context.WithTimeout(ctx, blockTransferQuery)
		data, status, ra, err := getBlock(attemptCtx, url, token)
		cancel()

		if err == nil && status >= 200 && status < 300 {
			return data, nil
		}
		if errors.Is(err, errBlockTooLarge) {
			return nil, err
		}
		if attempt >= blockMaxRetries {
			if err != nil {
				return nil, err
			}
			return nil, storageFailed("download block", status, nil)
		}

		switch {
		case err != nil:
			if werr := sleepCtx(ctx, backoffDelay(attempt)); werr != nil {
				return nil, werr
			}
		case status == http.StatusTooManyRequests:
			if werr := sleepCtx(ctx, retryAfter(ra, attempt)); werr != nil {
				return nil, werr
			}
		case status >= 500:
			if werr := sleepCtx(ctx, backoffDelay(attempt)); werr != nil {
				return nil, werr
			}
		default: // 4xx that isn't rate limiting: not recoverable
			return nil, storageFailed("download block", status, nil)
		}
	}
}

func (s *Service) requestBlockLinks(ctx context.Context, dc *Context, linkID, revisionID string, by author, batch []*encBlock) ([]uploadLink, error) {
	blockList := make([]map[string]any, len(batch))
	for i, b := range batch {
		blockList[i] = b.listEntry()
	}
	var res struct {
		UploadLinks []struct{ Token, BareURL string }
	}
	err := s.C.Decode(ctx, blockLinksRequest(dc, linkID, revisionID, by, blockList), &res)
	if err != nil {
		return nil, err
	}
	if len(res.UploadLinks) != len(batch) {
		return nil, fmt.Errorf("requested %d block links, got %d", len(batch), len(res.UploadLinks))
	}
	now := time.Now()
	links := make([]uploadLink, len(res.UploadLinks))
	for i, l := range res.UploadLinks {
		links[i] = uploadLink{Token: l.Token, BareURL: l.BareURL, created: now}
	}
	return links, nil
}

// blockLinksRequest asks for somewhere to put a batch of blocks, of whichever
// endpoint serves the tree the file is in.
//
// A share is named along with the address the blocks are written by. A link
// names the volume its items live on instead, and the address is nobody's where
// the link was opened without an account.
//
// Repeatable: asking a second time hands back a second set of links and changes
// nothing else, so a request whose answer was lost is worth making again rather
// than failing an upload part way through.
func blockLinksRequest(dc *Context, linkID, revisionID string, by author, blockList []map[string]any) proton.Request {
	body := map[string]any{"LinkID": linkID, "RevisionID": revisionID, "BlockList": blockList}
	if dc.Public() {
		body["VolumeID"], body["AddressID"] = dc.VolumeID, by.address()
		return proton.Request{
			Method: "POST", Path: "/drive/unauth/blocks", Repeatable: true, Body: body,
		}
	}
	body["AddressID"], body["ShareID"] = dc.AddrID, dc.ShareID
	return proton.Request{
		Method: "POST", Path: "/drive/blocks", Repeatable: true, Body: body,
	}
}
