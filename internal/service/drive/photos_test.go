package drive

import (
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
	"github.com/roman-16/proton-cli/internal/proton"
)

// stubDoer records the requests issued through the drive.Client seam and replays
// canned JSON bodies, so wire-format contracts can be asserted without the API.
//
// routes answers one request each, keyed by method and path; respBody answers
// whatever routes does not name, which is all a test of a single request needs.
// linkInfo and linkShare stand in for the SRP handshake, which is proved against
// Proton and cannot be canned.
type stubDoer struct {
	// mu guards reqs, because a command that asks for several things at once
	// records through one stub from a goroutine each.
	mu       sync.Mutex
	reqs     []proton.Request
	respBody []byte
	routes   map[string]string
	// answers is consulted before the canned routes, for an endpoint whose answer
	// depends on what was asked rather than on which path it was asked of.
	answers   func(proton.Request) []byte
	linkInfo  *proton.PublicLinkInfo
	linkShare *proton.PublicLinkShare
	proved    string
}

func (s *stubDoer) record(r proton.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reqs = append(s.reqs, r)
}

func (s *stubDoer) PublicLinkInfo(_ context.Context, token string) (*proton.PublicLinkInfo, error) {
	s.record(proton.Request{Method: "GET", Path: "/drive/urls/" + token + "/info"})
	if s.linkInfo == nil {
		return nil, errors.New("no public link info canned")
	}
	return s.linkInfo, nil
}

func (s *stubDoer) PublicLinkAuth(_ context.Context, token string, _ *proton.PublicLinkInfo, password string) (*proton.PublicLinkShare, error) {
	s.record(proton.Request{Method: "POST", Path: "/drive/urls/" + token + "/auth"})
	s.proved = password
	if s.linkShare == nil {
		return nil, errors.New("no public link share canned")
	}
	return s.linkShare, nil
}

func (s *stubDoer) body(r proton.Request) []byte {
	if s.answers != nil {
		if body := s.answers(r); body != nil {
			return body
		}
	}
	if canned, ok := s.routes[r.Method+" "+r.Path]; ok {
		return []byte(canned)
	}
	return s.respBody
}

func (s *stubDoer) Do(_ context.Context, r proton.Request) (*proton.Response, error) {
	s.record(r)
	return &proton.Response{Status: 200, Body: s.body(r)}, nil
}

func (s *stubDoer) Decode(_ context.Context, r proton.Request, out any) error {
	s.record(r)
	body := s.body(r)
	if out == nil || body == nil {
		return nil
	}
	return json.Unmarshal(body, out)
}

func (s *stubDoer) last() proton.Request { return s.reqs[len(s.reqs)-1] }

// sent reports whether a request was made, so a test can assert that one was not.
func (s *stubDoer) sent(method, path string) bool {
	for _, r := range s.reqs {
		if r.Method == method && r.Path == path {
			return true
		}
	}
	return false
}

// PhotosList must set the Tag param only when a filter is requested, and only
// to the requested id; a drifted param name or value would silently break the
// `--tags` filter.
func TestPhotosListTagFilterSetsTagParam(t *testing.T) {
	dc := &Context{VolumeID: "vol1"}

	t.Run("tag filter sets the Tag param", func(t *testing.T) {
		f := &stubDoer{respBody: []byte(`{"Photos":[]}`)}
		if _, err := New(f, testKeys(nil)).PhotosList(context.Background(), dc, 2, true); err != nil {
			t.Fatalf("PhotosList: %v", err)
		}
		if got := f.last().Query.Get("Tag"); got != "2" {
			t.Errorf("Tag = %q, want %q (videos)", got, "2")
		}
	})

	t.Run("unfiltered omits Tag", func(t *testing.T) {
		f := &stubDoer{respBody: []byte(`{"Photos":[]}`)}
		if _, err := New(f, testKeys(nil)).PhotosList(context.Background(), dc, 0, false); err != nil {
			t.Fatalf("PhotosList: %v", err)
		}
		if f.last().Query.Has("Tag") {
			t.Errorf("Tag should be absent when filter is false, got %q", f.last().Query.Get("Tag"))
		}
	})

	t.Run("tags surface as names, not ints", func(t *testing.T) {
		f := &stubDoer{respBody: []byte(`{"Photos":[{"LinkID":"l1","Tags":[0,2,42]}]}`)}
		photos, err := New(f, testKeys(nil)).PhotosList(context.Background(), dc, 0, false)
		if err != nil {
			t.Fatalf("PhotosList: %v", err)
		}
		if len(photos) != 1 {
			t.Fatalf("got %d photos, want 1", len(photos))
		}
		want := []string{"favorites", "videos", "42"}
		if len(photos[0].Tags) != len(want) {
			t.Fatalf("Tags = %v, want %v", photos[0].Tags, want)
		}
		for i, w := range want {
			if photos[0].Tags[i] != w {
				t.Errorf("Tags[%d] = %q, want %q", i, photos[0].Tags[i], w)
			}
		}
	})
}

// TagName/ParseTag are the single source of truth for the user-facing tag
// vocabulary; they must round-trip and reject anything that would leak a raw
// enum int to (or from) the user.
func TestTagNameAndParseTag(t *testing.T) {
	cases := map[int]string{
		0: "favorites", 1: "screenshots", 2: "videos", 3: "live-photos",
		4: "motion-photos", 5: "selfies", 6: "portraits", 7: "bursts",
		8: "panoramas", 9: "raw",
	}
	for id, name := range cases {
		if got := TagName(id); got != name {
			t.Errorf("TagName(%d) = %q, want %q", id, got, name)
		}
		got, err := ParseTag(name)
		if err != nil || got != id {
			t.Errorf("ParseTag(%q) = %d, %v; want %d, nil", name, got, err, id)
		}
	}
	// Unknown id falls back to its decimal form so future backend tags render.
	if got := TagName(42); got != "42" {
		t.Errorf("TagName(42) = %q, want \"42\"", got)
	}
	// Integer input is rejected: names only.
	if _, err := ParseTag("2"); err == nil {
		t.Error("ParseTag(\"2\") should reject integer input")
	}
	// An unknown name errors and lists the valid tags.
	_, err := ParseTag("selfie")
	if err == nil || !strings.Contains(err.Error(), "selfies") {
		t.Errorf("ParseTag(\"selfie\") err = %v, want it to list valid tags", err)
	}
}

func TestTagNames(t *testing.T) {
	got := tagNames([]int{0, 2, 42})
	want := []string{"favorites", "videos", "42"}
	if len(got) != len(want) {
		t.Fatalf("tagNames = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("tagNames[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	if tagNames(nil) != nil {
		t.Error("tagNames(nil) should be nil")
	}
}

// PhotosUnfavorite must DELETE the Favorites tag (0) from each link.
func TestPhotosUnfavoriteRemovesFavoriteTag(t *testing.T) {
	f := &stubDoer{}
	dc := &Context{VolumeID: "vol1"}
	if err := New(f, testKeys(nil)).PhotosUnfavorite(context.Background(), dc, []string{"link-a"}); err != nil {
		t.Fatalf("PhotosUnfavorite: %v", err)
	}
	req := f.last()
	if req.Method != "DELETE" {
		t.Errorf("method = %q, want DELETE", req.Method)
	}
	if req.Path != "/drive/photos/volumes/vol1/links/link-a/tags" {
		t.Errorf("path = %q", req.Path)
	}
	body, ok := req.Body.(map[string]any)
	if !ok {
		t.Fatalf("body is not map[string]any: %T", req.Body)
	}
	tags, ok := body["Tags"].([]int)
	if !ok || len(tags) != 1 || tags[0] != favoriteTag {
		t.Errorf("Tags = %v, want [%d] (favoriteTag)", body["Tags"], favoriteTag)
	}
}

// photoLibrary is a photo library as Proton hands it over: a root sealed to the
// share key, carrying the hash key every photo's name and content are hashed
// under.
func photoLibrary(t *testing.T) (*Context, []byte) {
	t.Helper()
	shareKey, err := pgp.GenerateKey("Photos", "", "x25519", 0)
	if err != nil {
		t.Fatalf("generate a share key: %v", err)
	}
	shareKR, err := pgp.NewKeyRing(shareKey)
	if err != nil {
		t.Fatal(err)
	}
	rootKey, rootPass, rootPassSig, rootPriv, err := genNodeKeys(shareKR, shareKR)
	if err != nil {
		t.Fatalf("generate a root key: %v", err)
	}
	rootKR, err := pgp.NewKeyRing(rootPriv)
	if err != nil {
		t.Fatal(err)
	}
	hashKey, armoured, err := genNodeHashKey(rootKR, rootKR)
	if err != nil {
		t.Fatalf("generate the root's hash key: %v", err)
	}
	return &Context{
		ShareID: testShareID, VolumeID: testVolumeID, RootLinkID: testRootID,
		ShareKR: shareKR, Type: shareTypePhotos,
		rootLink: &Link{
			LinkID: testRootID, Type: protonFolder, NodeKey: rootKey,
			NodePassphrase: rootPass, NodePassphraseSignature: rootPassSig,
			FolderProperties: &FolderProperties{NodeHashKey: armoured},
		},
	}, hashKey
}

// content is a photo's bytes as a file the plan can open again, and how many
// times it did.
type content struct {
	bytes  string
	opened int
}

func (c *content) open() (io.ReadCloser, error) {
	c.opened++
	return io.NopCloser(strings.NewReader(c.bytes)), nil
}

// hashes is what the library holds of one photo: its name hashed as written,
// and its content hashed the way a revision records it.
func hashes(t *testing.T, hashKey []byte, name, bytes string) (nameHash, contentHash string) {
	t.Helper()
	nameHash, err := lookupHash(name, hashKey)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha1.Sum([]byte(bytes)) //nolint:gosec // Proton records the content digest as SHA-1
	contentHash, err = lookupHash(hex.EncodeToString(sum[:]), hashKey)
	if err != nil {
		t.Fatal(err)
	}
	return nameHash, contentHash
}

func duplicatesAnswer(t *testing.T, found ...map[string]any) map[string]string {
	t.Helper()
	return map[string]string{
		"POST /drive/volumes/" + testVolumeID + "/photos/duplicates": object(t, map[string]any{"DuplicateHashes": found}),
	}
}

// A photo the library holds under the same name with the same content is found
// rather than uploaded again, which is the test Proton's own apps apply - and
// asking is a read, so a preview can say so too.
func TestAPhotoTheLibraryHoldsIsFoundRatherThanUploaded(t *testing.T) {
	dc, hashKey := photoLibrary(t)
	nameHash, contentHash := hashes(t, hashKey, "IMG_0001.JPG", "the photo")
	doer := &stubDoer{routes: duplicatesAnswer(t, map[string]any{
		"Hash": nameHash, "ContentHash": contentHash, "LinkState": 1, "LinkID": "photo-1",
	})}
	photo := &content{bytes: "the photo"}

	plan, err := New(doer, testKeys(nil)).PlanPhotoUpload(context.Background(), dc, "IMG_0001.JPG", photo.open)
	if err != nil {
		t.Fatalf("PlanPhotoUpload: %v", err)
	}

	if plan.Duplicate != "photo-1" {
		t.Errorf("Duplicate = %q, want the photo the library holds", plan.Duplicate)
	}
	req := doer.last()
	if !req.Reads {
		t.Error("the duplicate check does not say it only reads, so --dry-run would refuse it")
	}
	asked, _ := req.Body.(map[string]any)["NameHashes"].([]string)
	if len(asked) != 1 || asked[0] != nameHash {
		t.Errorf("asked about %v, want the name hashed exactly as written", asked)
	}
}

// The content decides only once a name matches, and a match that is a draft or
// in the trash is not a photo the library holds.
func TestOnlyALiveMatchOfNameAndContentIsADuplicate(t *testing.T) {
	dc, hashKey := photoLibrary(t)
	nameHash, contentHash := hashes(t, hashKey, "IMG_0001.JPG", "the photo")
	_, otherContent := hashes(t, hashKey, "IMG_0001.JPG", "another camera's photo")
	for _, tc := range []struct {
		name   string
		found  []map[string]any
		opened int
	}{
		{"no photo of that name", nil, 0},
		{"a different photo of that name", []map[string]any{
			{"Hash": nameHash, "ContentHash": otherContent, "LinkState": 1, "LinkID": "photo-2"},
		}, 1},
		{"the same photo, trashed", []map[string]any{
			{"Hash": nameHash, "ContentHash": contentHash, "LinkState": 2, "LinkID": "photo-3"},
		}, 0},
		{"the same photo, never finished", []map[string]any{
			{"Hash": nameHash, "ContentHash": contentHash, "LinkState": 0, "LinkID": "photo-4"},
		}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doer := &stubDoer{routes: duplicatesAnswer(t, tc.found...)}
			photo := &content{bytes: "the photo"}

			plan, err := New(doer, testKeys(nil)).PlanPhotoUpload(context.Background(), dc, "IMG_0001.JPG", photo.open)
			if err != nil {
				t.Fatalf("PlanPhotoUpload: %v", err)
			}

			if plan.Duplicate != "" {
				t.Errorf("Duplicate = %q, want none", plan.Duplicate)
			}
			if photo.opened != tc.opened {
				t.Errorf("the photo was read %d times, want %d", photo.opened, tc.opened)
			}
		})
	}
}

// A photo is streamed rather than held, and the content hash its revision
// records is made from the bytes that went up.
func TestAPhotoUploadRecordsTheContentItStreamed(t *testing.T) {
	dc, hashKey := photoLibrary(t)
	nameHash, contentHash := hashes(t, hashKey, "IMG_0001.JPG", "the photo")
	storage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(storage.Close)
	share := "/drive/shares/" + testShareID
	routes := duplicatesAnswer(t)
	routes["POST "+share+"/files"] = `{"File":{"ID":"file-1","RevisionID":"rev-1"}}`
	routes["GET "+share+"/links/file-1/revisions/rev-1/verification"] = object(t, map[string]any{
		"VerificationCode": base64.StdEncoding.EncodeToString([]byte("verify-these-bytes")),
	})
	routes["POST /drive/blocks"] = object(t, map[string]any{
		"UploadLinks": []any{map[string]any{"Token": "block-token", "BareURL": storage.URL}},
	})
	routes["PUT "+share+"/files/file-1/revisions/rev-1"] = `{}`
	doer := &stubDoer{routes: routes}
	s := New(doer, testKeys(nil))
	photo := &content{bytes: "the photo"}

	plan, err := s.PlanPhotoUpload(context.Background(), dc, "IMG_0001.JPG", photo.open)
	if err != nil {
		t.Fatalf("PlanPhotoUpload: %v", err)
	}
	if err := s.PhotoUpload(context.Background(), dc, plan, 1700000000, UploadOptions{}); err != nil {
		t.Fatalf("PhotoUpload: %v", err)
	}

	var draft, commit map[string]any
	for _, req := range doer.reqs {
		body, _ := req.Body.(map[string]any)
		switch {
		case req.Method == "POST" && req.Path == share+"/files":
			draft = body
		case req.Method == "PUT":
			commit = body
		}
	}
	if draft["Hash"] != nameHash {
		t.Errorf("the photo's name hashes to %v, want the name hashed exactly as written", draft["Hash"])
	}
	described, _ := commit["Photo"].(map[string]any)
	if described["ContentHash"] != contentHash {
		t.Errorf("the revision records content hash %v, want the hash of what was streamed", described["ContentHash"])
	}
	if described["CaptureTime"] != int64(1700000000) {
		t.Errorf("the revision records capture time %v", described["CaptureTime"])
	}
	if photo.opened != 1 {
		t.Errorf("the photo was read %d times; with no photo of its name in the library, once is enough", photo.opened)
	}
}
