package drive

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/roman-16/proton-cli/internal/proton"
)

// stubDoer records the requests issued through the proton.Doer seam and replays
// canned JSON bodies, so wire-format contracts can be asserted without the API.
//
// routes answers one request each, keyed by method and path; respBody answers
// whatever routes does not name, which is all a test of a single request needs.
type stubDoer struct {
	reqs     []proton.Request
	respBody []byte
	routes   map[string]string
}

func (s *stubDoer) body(r proton.Request) []byte {
	if canned, ok := s.routes[r.Method+" "+r.Path]; ok {
		return []byte(canned)
	}
	return s.respBody
}

func (s *stubDoer) Do(_ context.Context, r proton.Request) (*proton.Response, error) {
	s.reqs = append(s.reqs, r)
	return &proton.Response{Status: 200, Body: s.body(r)}, nil
}

func (s *stubDoer) Decode(_ context.Context, r proton.Request, out any) error {
	s.reqs = append(s.reqs, r)
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
