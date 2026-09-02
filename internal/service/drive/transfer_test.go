package drive

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/roman-16/proton-cli/internal/proton"
)

// shrinkBackoff makes retry backoff negligible for the duration of a test so
// the retry paths run fast, restoring the production value afterwards.
func shrinkBackoff(t *testing.T) {
	t.Helper()
	prev := blockRetryBaseDelay
	blockRetryBaseDelay = time.Microsecond
	t.Cleanup(func() { blockRetryBaseDelay = prev })
}

func failRefresh(t *testing.T) func(context.Context) (uploadLink, error) {
	return func(context.Context) (uploadLink, error) {
		t.Helper()
		t.Error("refresh should not be called")
		return uploadLink{}, errors.New("unexpected refresh")
	}
}

func TestBackoffDelayGrowsAndCaps(t *testing.T) {
	prev := blockRetryBaseDelay
	blockRetryBaseDelay = time.Second
	t.Cleanup(func() { blockRetryBaseDelay = prev })

	tests := []struct {
		attempt int
		want    time.Duration
	}{
		{0, time.Second},
		{1, 2 * time.Second},
		{2, 4 * time.Second},
		{20, 30 * time.Second}, // capped
	}
	for _, tc := range tests {
		if got := backoffDelay(tc.attempt); got != tc.want {
			t.Errorf("backoffDelay(%d) = %v, want %v", tc.attempt, got, tc.want)
		}
	}
}

func TestRetryAfter(t *testing.T) {
	prev := blockRetryBaseDelay
	blockRetryBaseDelay = time.Second
	t.Cleanup(func() { blockRetryBaseDelay = prev })

	if got := retryAfter("2", 0); got != 2*time.Second {
		t.Errorf("retryAfter(2) = %v, want 2s", got)
	}
	if got := retryAfter("0", 0); got != 0 {
		t.Errorf("retryAfter(0) = %v, want 0", got)
	}
	if got := retryAfter("999999", 0); got != blockMaxRateLimit {
		t.Errorf("retryAfter(huge) = %v, want cap %v", got, blockMaxRateLimit)
	}
	if got := retryAfter("", 3); got != backoffDelay(3) {
		t.Errorf("retryAfter(empty) = %v, want backoff %v", got, backoffDelay(3))
	}
	if got := retryAfter("nonsense", 1); got != backoffDelay(1) {
		t.Errorf("retryAfter(nonsense) = %v, want backoff %v", got, backoffDelay(1))
	}
}

func TestTokenRejected(t *testing.T) {
	for _, s := range []int{401, 403, 404, 409, 410, 422} {
		if !tokenRejected(s) {
			t.Errorf("HTTP %d should be token-rejected", s)
		}
	}
	for _, s := range []int{200, 400, 429, 500, 502} {
		if tokenRejected(s) {
			t.Errorf("HTTP %d should not be token-rejected", s)
		}
	}
}

// TestUploadBlockRetriesTransientThenSucceeds is the Jul-20 regression: a
// single transient 502 mid-upload must be retried, not abort the transfer.
func TestUploadBlockRetriesTransientThenSucceeds(t *testing.T) {
	shrinkBackoff(t)
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&hits, 1) < 3 {
			http.Error(w, "boom", http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	link := uploadLink{Token: "tok-1", BareURL: srv.URL, created: time.Now()}
	tok, err := uploadBlock(context.Background(), 1, []byte("data"), link, failRefresh(t))
	if err != nil {
		t.Fatalf("uploadBlock: %v", err)
	}
	if tok != "tok-1" {
		t.Errorf("token = %q, want tok-1", tok)
	}
	if got := atomic.LoadInt32(&hits); got != 3 {
		t.Errorf("server hits = %d, want 3", got)
	}
}

func TestUploadBlockHonorsRateLimit(t *testing.T) {
	shrinkBackoff(t)
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&hits, 1) == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	link := uploadLink{Token: "t", BareURL: srv.URL, created: time.Now()}
	if _, err := uploadBlock(context.Background(), 1, []byte("d"), link, failRefresh(t)); err != nil {
		t.Fatalf("uploadBlock: %v", err)
	}
	if got := atomic.LoadInt32(&hits); got != 2 {
		t.Errorf("server hits = %d, want 2", got)
	}
}

func TestUploadBlockRefreshesOnTokenRejection(t *testing.T) {
	shrinkBackoff(t)
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		if r.Header.Get("pm-storage-token") == "stale" {
			http.Error(w, "gone", http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	var refreshed int32
	refresh := func(context.Context) (uploadLink, error) {
		atomic.AddInt32(&refreshed, 1)
		return uploadLink{Token: "fresh", BareURL: srv.URL, created: time.Now()}, nil
	}
	link := uploadLink{Token: "stale", BareURL: srv.URL, created: time.Now()}
	tok, err := uploadBlock(context.Background(), 7, []byte("d"), link, refresh)
	if err != nil {
		t.Fatalf("uploadBlock: %v", err)
	}
	if tok != "fresh" {
		t.Errorf("token = %q, want fresh (from refreshed link)", tok)
	}
	if got := atomic.LoadInt32(&refreshed); got != 1 {
		t.Errorf("refresh calls = %d, want 1", got)
	}
	if got := atomic.LoadInt32(&hits); got != 2 {
		t.Errorf("server hits = %d, want 2", got)
	}
}

// TestUploadBlockProactiveTokenRefresh refreshes a link whose token is already
// older than the TTL before even attempting the upload.
func TestUploadBlockProactiveTokenRefresh(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("pm-storage-token") != "fresh" {
			http.Error(w, "stale token used", http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	refresh := func(context.Context) (uploadLink, error) {
		return uploadLink{Token: "fresh", BareURL: srv.URL, created: time.Now()}, nil
	}
	stale := uploadLink{Token: "stale", BareURL: srv.URL, created: time.Now().Add(-2 * blockTokenTTL)}
	tok, err := uploadBlock(context.Background(), 1, []byte("d"), stale, refresh)
	if err != nil {
		t.Fatalf("uploadBlock: %v", err)
	}
	if tok != "fresh" {
		t.Errorf("token = %q, want fresh (stale token should be refreshed first)", tok)
	}
}

func TestUploadBlockFatalOn4xx(t *testing.T) {
	shrinkBackoff(t)
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		http.Error(w, "bad", http.StatusBadRequest)
	}))
	defer srv.Close()

	link := uploadLink{Token: "t", BareURL: srv.URL, created: time.Now()}
	if _, err := uploadBlock(context.Background(), 1, []byte("d"), link, failRefresh(t)); err == nil {
		t.Fatal("expected an error on HTTP 400")
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Errorf("server hits = %d, want 1 (400 is not retried)", got)
	}
}

func TestUploadBlockExhaustsRetries(t *testing.T) {
	shrinkBackoff(t)
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		http.Error(w, "boom", http.StatusBadGateway)
	}))
	defer srv.Close()

	link := uploadLink{Token: "t", BareURL: srv.URL, created: time.Now()}
	if _, err := uploadBlock(context.Background(), 1, []byte("d"), link, failRefresh(t)); err == nil {
		t.Fatal("expected an error after exhausting retries")
	}
	if got := atomic.LoadInt32(&hits); got != blockMaxRetries+1 {
		t.Errorf("server hits = %d, want %d", got, blockMaxRetries+1)
	}
}

func TestUploadBlockRespectsContextCancel(t *testing.T) {
	prev := blockRetryBaseDelay
	blockRetryBaseDelay = time.Hour // force a long backoff so cancellation wins
	t.Cleanup(func() { blockRetryBaseDelay = prev })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusBadGateway)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	link := uploadLink{Token: "t", BareURL: srv.URL, created: time.Now()}
	_, err := uploadBlock(ctx, 1, []byte("d"), link, failRefresh(t))
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

func TestDownloadBlockRetriesThenSucceeds(t *testing.T) {
	shrinkBackoff(t)
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&hits, 1) < 3 {
			http.Error(w, "boom", http.StatusBadGateway)
			return
		}
		_, _ = w.Write([]byte("block-bytes"))
	}))
	defer srv.Close()

	data, err := downloadBlock(context.Background(), srv.URL, "tok")
	if err != nil {
		t.Fatalf("downloadBlock: %v", err)
	}
	if string(data) != "block-bytes" {
		t.Errorf("data = %q, want block-bytes", data)
	}
	if got := atomic.LoadInt32(&hits); got != 3 {
		t.Errorf("server hits = %d, want 3", got)
	}
}

// TestBuildRevisionCommitOrdersByIndex proves the manifest and BlockList stay
// index-ordered even though parallel workers record results out of order.
func TestBuildRevisionCommitOrdersByIndex(t *testing.T) {
	rawHashByIdx := map[int][]byte{3: {0x03}, 1: {0x01}, 2: {0x02}}
	tokenByIdx := map[int]string{3: "t3", 1: "t1", 2: "t2"}

	manifest, list, err := buildRevisionCommit(rawHashByIdx, tokenByIdx)
	if err != nil {
		t.Fatalf("buildRevisionCommit: %v", err)
	}
	if !bytes.Equal(manifest, []byte{0x01, 0x02, 0x03}) {
		t.Errorf("manifest = %v, want [1 2 3] (hashes in index order)", manifest)
	}
	if len(list) != 3 {
		t.Fatalf("blockList len = %d, want 3", len(list))
	}
	for i, want := range []struct {
		idx int
		tok string
	}{{1, "t1"}, {2, "t2"}, {3, "t3"}} {
		if list[i]["Index"] != want.idx {
			t.Errorf("list[%d].Index = %v, want %d", i, list[i]["Index"], want.idx)
		}
		if list[i]["Token"] != want.tok {
			t.Errorf("list[%d].Token = %v, want %s", i, list[i]["Token"], want.tok)
		}
	}
}

func TestBuildRevisionCommitDetectsGap(t *testing.T) {
	rawHashByIdx := map[int][]byte{1: {0x01}, 3: {0x03}} // block 2 missing
	tokenByIdx := map[int]string{1: "t1", 3: "t3"}
	if _, _, err := buildRevisionCommit(rawHashByIdx, tokenByIdx); err == nil {
		t.Error("expected an error for a gap in block indices")
	}
}

func TestXorVerifier(t *testing.T) {
	ver := []byte{0xff, 0x0f, 0xaa, 0x55}
	enc := []byte{0x0f, 0xf0} // shorter than the verification code
	got := xorVerifier(ver, enc)
	want := []byte{0xf0, 0xff, 0xaa, 0x55} // first two XORed, remainder unchanged
	if !bytes.Equal(got, want) {
		t.Errorf("xorVerifier = %v, want %v", got, want)
	}
}

// seqDoer is a proton.Doer that replays a scripted sequence of Decode outcomes,
// so requestBlockLinks retry behaviour can be tested without the network.
type seqDoer struct {
	calls int
	steps []doerStep
	seen  []proton.Request
}

type doerStep struct {
	body string
	err  error
}

func (d *seqDoer) Do(context.Context, proton.Request) (*proton.Response, error) {
	return nil, errors.New("Do unused")
}

func (d *seqDoer) Decode(_ context.Context, req proton.Request, out any) error {
	d.seen = append(d.seen, req)
	s := d.steps[len(d.steps)-1]
	if d.calls < len(d.steps) {
		s = d.steps[d.calls]
	}
	d.calls++
	if s.err != nil {
		return s.err
	}
	if out == nil || s.body == "" {
		return nil
	}
	return json.Unmarshal([]byte(s.body), out)
}

// Asking for links a second time hands back a second set and changes nothing
// else, so the request says it may be sent again and an upload survives a bad
// moment mid-file. The asking again itself is the client's, not this service's.
func TestRequestBlockLinksMayBeAskedAgain(t *testing.T) {
	d := &seqDoer{steps: []doerStep{
		{body: `{"UploadLinks":[{"Token":"a","BareURL":"http://x/1"},{"Token":"b","BareURL":"http://x/2"}]}`},
	}}
	links, err := New(d, testKeys(nil)).requestBlockLinks(context.Background(), "sh", "lk", "rev", "ad",
		[]*encBlock{{index: 1}, {index: 2}})
	if err != nil {
		t.Fatalf("requestBlockLinks: %v", err)
	}
	if len(links) != 2 || links[0].Token != "a" || links[1].Token != "b" {
		t.Errorf("links = %+v, want tokens a,b", links)
	}
	if len(d.seen) != 1 || !d.seen[0].Repeatable {
		t.Errorf("request = %+v, want one marked repeatable", d.seen)
	}
}

func TestRequestBlockLinksCountMismatch(t *testing.T) {
	d := &seqDoer{steps: []doerStep{
		{body: `{"UploadLinks":[{"Token":"a","BareURL":"u"}]}`},
	}}
	_, err := New(d, testKeys(nil)).requestBlockLinks(context.Background(), "s", "l", "r", "a",
		[]*encBlock{{index: 1}, {index: 2}})
	if err == nil {
		t.Fatal("expected a count-mismatch error (2 blocks, 1 link)")
	}
}

func TestRequestBlockLinksNonRetryable(t *testing.T) {
	d := &seqDoer{steps: []doerStep{{err: &proton.APIError{HTTPStatus: 404}}}}
	_, err := New(d, testKeys(nil)).requestBlockLinks(context.Background(), "s", "l", "r", "a",
		[]*encBlock{{index: 1}})
	if err == nil {
		t.Fatal("expected an error")
	}
	if d.calls != 1 {
		t.Errorf("Decode calls = %d, want 1 (404 is not retried)", d.calls)
	}
}

// A storage token authorises the block it was issued for on the host Proton
// named, and net/http would carry a header it does not recognise as sensitive
// to wherever a redirect pointed.
func TestABlockTransferDoesNotCarryItsTokenToAnotherHost(t *testing.T) {
	var elsewhere atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		elsewhere.Add(1)
		if r.Header.Get("pm-storage-token") != "" {
			t.Error("the storage token followed a redirect to another host")
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer other.Close()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL, http.StatusFound)
	}))
	defer srv.Close()

	shrinkBackoff(t)
	link := uploadLink{Token: "t", BareURL: srv.URL, created: time.Now()}
	if _, err := uploadBlock(context.Background(), 1, []byte("d"), link, failRefresh(t)); err == nil {
		t.Error("a redirect off the origin should fail rather than be followed")
	}
	if got := elsewhere.Load(); got != 0 {
		t.Errorf("the other host was reached %d times, want 0", got)
	}
}

// A block is a known size, and the bytes are held whole while they are
// decrypted, so an answer that keeps coming is refused rather than read.
func TestABlockLargerThanABlockIsRefused(t *testing.T) {
	shrinkBackoff(t)
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(make([]byte, storedBlockLimit+1))
	}))
	defer srv.Close()

	_, err := downloadBlock(context.Background(), srv.URL, "t")
	if !errors.Is(err, errBlockTooLarge) {
		t.Fatalf("err = %v, want the oversize block refusal", err)
	}
	if got := hits.Load(); got != 1 {
		t.Errorf("server hits = %d, want 1 (asking again would fetch the same thing)", got)
	}
}

// What a block transfer failed with is said without quoting the signed URL it
// was talking to, which is a credential for the block it names.
func TestAStorageFailureQuotesNoUrlOrBody(t *testing.T) {
	shrinkBackoff(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"URL":"https://storage.proton.me/x?token=eyJhbGciOi"}`, http.StatusBadRequest)
	}))
	defer srv.Close()

	link := uploadLink{Token: "sensitive-token", BareURL: srv.URL, created: time.Now()}
	_, err := uploadBlock(context.Background(), 3, []byte("d"), link, failRefresh(t))
	if err == nil {
		t.Fatal("expected an error on HTTP 400")
	}
	said := err.Error()
	for _, secret := range []string{srv.URL, "sensitive-token", "eyJhbGciOi"} {
		if strings.Contains(said, secret) {
			t.Errorf("the failure quotes %q: %s", secret, said)
		}
	}
	for _, want := range []string{"upload block 3", "400"} {
		if !strings.Contains(said, want) {
			t.Errorf("the failure does not say %q: %s", want, said)
		}
	}
}
