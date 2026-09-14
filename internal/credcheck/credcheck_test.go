package credcheck

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// hashOf is what the corpus would file a password under, so a test can build a
// bucket the way the corpus does.
func hashOf(password string) (prefix, suffix string) {
	sum := sha1.Sum([]byte(password))
	h := strings.ToUpper(hex.EncodeToString(sum[:]))
	return h[:prefixLength], h[prefixLength:]
}

func gzipped(t *testing.T, body string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	if _, err := w.Write([]byte(body)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// corpus stands in for the published buckets, and records what it was asked for
// so a test can assert that the password itself was never part of the question.
type corpus struct {
	bucket  []byte
	status  int
	asked   []string
	headers http.Header
}

func (c *corpus) serve(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.asked = append(c.asked, r.URL.Path)
		c.headers = r.Header.Clone()
		if c.status != 0 && c.status != http.StatusOK {
			w.WriteHeader(c.status)
			return
		}
		_, _ = w.Write(c.bucket)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// at points Compromised at the stand-in, which is the one thing the package
// cannot be told from outside - so the check runs against a rewritten base.
func at(t *testing.T, srv *httptest.Server, password string) (bool, error) {
	t.Helper()
	client := srv.Client()
	client.Transport = rewrite{to: srv.URL, inner: srv.Client().Transport}
	return Compromised(context.Background(), client, password)
}

// rewrite sends whatever host the package names to the stand-in instead.
type rewrite struct {
	to    string
	inner http.RoundTripper
}

func (r rewrite) RoundTrip(req *http.Request) (*http.Response, error) {
	target := req.Clone(req.Context())
	to, err := http.NewRequest(req.Method, r.to+req.URL.Path, nil)
	if err != nil {
		return nil, err
	}
	target.URL = to.URL
	target.Host = to.Host
	return r.inner.RoundTrip(target)
}

func TestCompromisedFindsAPasswordItsBucketNames(t *testing.T) {
	const password = "hunter2hunter2"
	_, suffix := hashOf(password)
	c := &corpus{bucket: gzipped(t, "0000000000000000000000000000000000:2\n"+suffix+":91\n")}

	found, err := at(t, c.serve(t), password)
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Error("the bucket names this hash, so the password is compromised")
	}
}

func TestCompromisedClearsAPasswordItsBucketDoesNotName(t *testing.T) {
	c := &corpus{bucket: gzipped(t, "0000000000000000000000000000000000:2\n")}
	found, err := at(t, c.serve(t), "hunter2hunter2")
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Error("the bucket does not name this hash")
	}
}

func TestAnEmptyBucketIsAPasswordThatHasNotLeaked(t *testing.T) {
	c := &corpus{status: http.StatusNotFound}
	found, err := at(t, c.serve(t), "hunter2hunter2")
	if err != nil {
		t.Fatalf("a bucket nobody published is not a failure: %v", err)
	}
	if found {
		t.Error("nothing was published, so nothing matched")
	}
}

func TestAnUnhappyCorpusIsAnErrorAndNotAVerdict(t *testing.T) {
	c := &corpus{status: http.StatusInternalServerError}
	if _, err := at(t, c.serve(t), "hunter2hunter2"); err == nil {
		t.Fatal("a corpus that would not answer should fail rather than clear the password")
	} else if !strings.Contains(err.Error(), Host) {
		t.Errorf("the failure should name the host it could not reach: %v", err)
	}
}

// The whole premise: six characters of a hash go out, and nothing else.
func TestOnlyTheBucketLeavesTheMachine(t *testing.T) {
	const password = "hunter2hunter2"
	prefix, suffix := hashOf(password)
	c := &corpus{bucket: gzipped(t, suffix+":91\n")}
	if _, err := at(t, c.serve(t), password); err != nil {
		t.Fatal(err)
	}

	asked := strings.Join(c.asked, " ")
	if !strings.Contains(asked, prefix) {
		t.Fatalf("the request should name the bucket %q: %s", prefix, asked)
	}
	if strings.Contains(asked, password) || strings.Contains(asked, suffix) {
		t.Errorf("the request carried more than the bucket: %s", asked)
	}
	if c.headers.Get("Add-Padding") != "true" {
		t.Error("the request should ask for padding, so a bucket's size says nothing")
	}
}
