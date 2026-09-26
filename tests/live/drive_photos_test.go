package live

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The photo library, which is a share of its own rather than part of the tree.
//
// A photo has no name in a listing, so a test finds the one it uploaded by what
// the upload added - which is why there is one helper for putting one there.

func writePNG(t *testing.T, path string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if err := png.Encode(f, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
}

// uploadedPhoto puts a photo in the library and hands back the ID it landed
// under, registering the cleanup that removes it again.
//
// A photo has no name in a listing, so what the upload added is the only way to
// tell it from the rest - which is why this is one helper rather than the same
// twenty lines in every test. Uploading is also what bootstraps the library, so
// nothing has to check whether the account has one first.
func uploadedPhoto(t *testing.T) string {
	t.Helper()
	id, _ := uploadedPhotoNamed(t, testID()+".png")
	return id
}

// uploadedPhotoNamed is uploadedPhoto under a name of the test's choosing, and
// hands back the file it uploaded as well, for a test about uploading it again.
func uploadedPhotoNamed(t *testing.T, name string) (id, path string) {
	t.Helper()
	before := photoLinkIDs(t)
	img := filepath.Join(t.TempDir(), name)
	writePNG(t, img)
	runOK(t, "drive", "photos", "upload", img)

	var photoID string
	waitFor(20*time.Second, 1*time.Second, func() bool {
		for id := range photoLinkIDs(t) {
			if !before[id] {
				photoID = id
				return true
			}
		}
		return false
	})
	if photoID == "" {
		t.Fatal("the uploaded photo did not appear in the listing")
	}
	cleanupRun(t, fmt.Sprintf("Delete photo: proton drive photos delete %s", photoID),
		"drive", "photos", "delete", "--", photoID)
	return photoID, img
}

// A photo the library already holds is not uploaded again, and saying so names
// the photo it already is. The name carries capitals, which is how a camera
// names a file.
func TestDrivePhotosUploadLeavesAPhotoTheLibraryHoldsAlone(t *testing.T) {
	photoID, img := uploadedPhotoNamed(t, testID()+"-IMG.PNG")
	held := photoLinkIDs(t)

	_, preview := runOKStderr(t, "--dry-run", "drive", "photos", "upload", img)
	assertContains(t, preview, "Dry run - nothing to upload")

	_, stderr := runOKStderr(t, "drive", "photos", "upload", img)
	assertContains(t, stderr, "Nothing to upload")
	assertContains(t, stderr, "is already in your photo library as")

	result := runJSON(t, "drive", "photos", "upload", img)
	if result["duplicate_of"] != photoID {
		t.Errorf("duplicate_of = %v, want the photo already in the library, %s", result["duplicate_of"], photoID)
	}
	if result["count"] != float64(0) {
		t.Errorf("count = %v, want nothing uploaded", result["count"])
	}
	for id := range photoLinkIDs(t) {
		if !held[id] {
			t.Errorf("a second copy landed as %s", id)
		}
	}
}

// A photo of the same name with other content is a different photo, and the
// library holds both: cameras reuse names.
func TestDrivePhotosUploadKeepsADifferentPhotoOfTheSameName(t *testing.T) {
	firstID, img := uploadedPhotoNamed(t, testID()+"-IMG.PNG")
	held := photoLinkIDs(t)
	f, err := os.Create(img)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()

	_, stderr := runOKStderr(t, "drive", "photos", "upload", img)

	assertContains(t, stderr, "Uploaded")
	var secondID string
	waitFor(20*time.Second, 1*time.Second, func() bool {
		for id := range photoLinkIDs(t) {
			if !held[id] {
				secondID = id
				return true
			}
		}
		return false
	})
	if secondID == "" {
		t.Fatal("the second photo did not appear in the listing")
	}
	cleanupRun(t, fmt.Sprintf("Delete photo: proton drive photos delete %s", secondID),
		"drive", "photos", "delete", "--", secondID)
	if !photoLinkIDs(t)[firstID] {
		t.Error("uploading a different photo of the same name took the first one away")
	}
}

// createdAlbum makes an album and hands back the ID it landed under,
// registering the cleanup that removes it again.
//
// An album is found the way a photo is, by what the creation added: the listing
// is the only thing that says which album is which.
func createdAlbum(t *testing.T, name string) string {
	t.Helper()
	before := map[string]bool{}
	for _, a := range runJSONArray(t, "drive", "photos", "albums", "list") {
		before[a.(map[string]interface{})["link_id"].(string)] = true
	}
	runOK(t, "drive", "photos", "albums", "create", "--name", name)

	var albumID string
	for _, a := range runJSONArray(t, "drive", "photos", "albums", "list") {
		m := a.(map[string]interface{})
		id := m["link_id"].(string)
		if before[id] {
			continue
		}
		albumID = id
		if seen, _ := m["name"].(string); seen != name {
			t.Errorf("album name: got %q want %q", seen, name)
		}
	}
	if albumID == "" {
		t.Fatal("created album not found in listing")
	}
	cleanupRun(t, fmt.Sprintf("Delete album: proton drive photos albums delete %s", albumID),
		"drive", "photos", "albums", "delete", "--", albumID)
	return albumID
}

func photoLinkIDs(t *testing.T) map[string]bool {
	t.Helper()
	set := map[string]bool{}
	for _, p := range runJSONArray(t, "drive", "photos", "list") {
		if id, ok := p.(map[string]interface{})["link_id"].(string); ok {
			set[id] = true
		}
	}
	return set
}

func TestDrivePhotosWriteLifecycle(t *testing.T) {
	photoID := uploadedPhoto(t)
	dir := t.TempDir()

	// A photo comes back as a named file or into a directory, and both have to
	// write something.
	outFile := filepath.Join(dir, "photo.out")
	runOK(t, "drive", "photos", "download", "--dest", outFile, photoID)
	if fi, err := os.Stat(outFile); err != nil || fi.Size() == 0 {
		t.Errorf("photos download --dest produced no file: %v", err)
	}
	outDir := filepath.Join(dir, "pics")
	runOK(t, "drive", "photos", "download", "--dest-dir", outDir, photoID)
	if entries, err := os.ReadDir(outDir); err != nil || len(entries) == 0 {
		t.Errorf("photos download --dest-dir wrote no file: %v", err)
	}

	albumID := createdAlbum(t, testID()+"-album")

	// Add the photo to the album (node-passphrase re-wrap), verify, remove.
	runOK(t, "drive", "photos", "albums", "add", albumID, photoID)
	found := false
	for _, it := range runJSONArray(t, "drive", "photos", "list", "--album", albumID) {
		if it.(map[string]interface{})["link_id"] == photoID {
			found = true
		}
	}
	if !found {
		t.Errorf("photo %s not found in album items", photoID)
	}
	runOK(t, "drive", "photos", "albums", "remove", albumID, photoID)
}

func photoInFavorites(t *testing.T, photoID string) bool {
	t.Helper()
	for _, p := range runJSONArray(t, "drive", "photos", "list", "--tag", "favorites") {
		if p.(map[string]interface{})["link_id"] == photoID {
			return true
		}
	}
	return false
}

// favoritePhotoTags returns the tag names JSON-listed for a favorited photo,
// proving tags surface as names (e.g. "favorites") rather than raw ints.
func favoritePhotoTags(t *testing.T, photoID string) []string {
	t.Helper()
	for _, p := range runJSONArray(t, "drive", "photos", "list", "--tag", "favorites") {
		m := p.(map[string]interface{})
		if m["link_id"] != photoID {
			continue
		}
		raw, _ := m["tags"].([]interface{})
		out := make([]string, 0, len(raw))
		for _, tg := range raw {
			if s, ok := tg.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// Trashing a photo takes it out of the timeline; deleting it is permanent and
// is what clears up afterwards.
func TestDrivePhotosTrash(t *testing.T) {
	photoID := uploadedPhoto(t)

	runOK(t, "drive", "photos", "trash", "--", photoID)
	if !waitFor(15*time.Second, 1*time.Second, func() bool { return !photoLinkIDs(t)[photoID] }) {
		t.Errorf("trashed photo %s still appears in the timeline listing", photoID)
	}
}

func TestDrivePhotosFavoriteRoundTrip(t *testing.T) {
	before := photoLinkIDs(t)

	dir := t.TempDir()
	img := filepath.Join(dir, testID()+".png")
	writePNG(t, img)
	runOK(t, "drive", "photos", "upload", img)

	var photoID string
	waitFor(20*time.Second, 1*time.Second, func() bool {
		for id := range photoLinkIDs(t) {
			if !before[id] {
				photoID = id
				return true
			}
		}
		return false
	})
	if photoID == "" {
		t.Fatal("uploaded photo did not appear in the listing")
	}
	cleanupRun(t, fmt.Sprintf("Delete photo: proton drive photos delete %s", photoID),
		"drive", "photos", "delete", "--", photoID)

	// --dry-run must not favorite.
	_, stderr := runOKStderr(t, "--dry-run", "drive", "photos", "favorite", "--", photoID)
	assertContains(t, stderr, "Dry run")
	if photoInFavorites(t, photoID) {
		t.Error("dry-run favorite should not actually favorite the photo")
	}

	// A freshly uploaded timeline photo is favorited in place (empty body).
	runOK(t, "drive", "photos", "favorite", "--", photoID)
	if !waitFor(20*time.Second, 1*time.Second, func() bool { return photoInFavorites(t, photoID) }) {
		t.Error("photo did not appear under --tags favorites after favorite")
	}
	// Tags are surfaced by name, never as raw ints.
	tags := favoritePhotoTags(t, photoID)
	hasFav := false
	for _, tg := range tags {
		if tg == "favorites" {
			hasFav = true
		}
	}
	if !hasFav {
		t.Errorf("favorited photo tags = %v, want to contain the name \"favorites\"", tags)
	}
	// Text mode (forced TTY) also renders the tag name, never a raw int.
	ttyOut, _, _ := runWithEnv(t, map[string]string{"PROTON_CLI_FORCE_TTY": "1"}, "drive", "photos", "list", "--tag", "favorites")
	if !strings.Contains(ttyOut, "favorites") {
		t.Errorf("text-mode list --tags favorites should show the 'favorites' tag name; got:\n%s", truncateOutput(ttyOut))
	}

	// --dry-run must not unfavorite either.
	_, stderr = runOKStderr(t, "--dry-run", "drive", "photos", "unfavorite", "--", photoID)
	assertContains(t, stderr, "Dry run")
	if !photoInFavorites(t, photoID) {
		t.Error("dry-run unfavorite should not actually remove the favorite")
	}

	runOK(t, "drive", "photos", "unfavorite", "--", photoID)
	if !waitFor(20*time.Second, 1*time.Second, func() bool { return !photoInFavorites(t, photoID) }) {
		t.Error("photo still under --tags favorites after unfavorite")
	}
}

// Which tag names exist is judged from the command line, so that is asserted in
// the offline suite. What is left here is that a name Proton knows actually
// filters against the library.
func TestDrivePhotosListTags(t *testing.T) {
	uploadedPhoto(t)
	runOK(t, "drive", "photos", "list", "--tag", "videos")
}

// An album's cover is which of its own photos represents it, so a photo that is
// not in the album is refused rather than stored as a reference nothing resolves.
func TestDrivePhotoAlbumCover(t *testing.T) {
	name := testID() + "-cover"
	albumID := strings.TrimSpace(runOK(t, "drive", "photos", "albums", "create", "--name", name))
	cleanupRun(t, fmt.Sprintf("Delete album: proton drive photos albums delete %s", albumID),
		"drive", "photos", "albums", "delete", "--", albumID)

	// A photo of this test's own, so the cover is something it made rather than
	// whichever of the library's the listing happened to put first.
	photoID := uploadedPhoto(t)
	runOK(t, "drive", "photos", "albums", "add", albumID, photoID)
	runOK(t, "drive", "photos", "albums", "update", "--cover", photoID, "--", albumID)

	// A photo outside the album cannot represent it, so the reference is refused
	// rather than stored as one nothing resolves.
	_, stderr, code := run(t, "drive", "photos", "albums", "update",
		"--cover", "notaphoto", "--", albumID)
	if code == 0 {
		t.Error("a cover that is not in the album should be refused")
	}
	if code == 0 || stderr == "" {
		t.Errorf("the refusal says nothing: %q", stderr)
	}
}

// ── sharing ──
//
// An album is shared with people and a photo either way, so what is asserted
// here is the half the file tree's tests cannot: that Proton takes a share on
// the photo volume, that an album arrives at the other account as an album, and
// that the photos inside it can be read there.

// TestDriveAlbumShareMemberRoundTrip invites a real Proton address to an album,
// changes what it may do, and withdraws it again.
func TestDriveAlbumShareMemberRoundTrip(t *testing.T) {
	invitee := secondaryEmail()
	albumID := createdAlbum(t, testID()+"-shared-album")
	runOK(t, "drive", "photos", "albums", "add", albumID, uploadedPhoto(t))

	runOK(t, "drive", "photos", "albums", "share", "add", "--", albumID, invitee)
	cleanupRun(t, fmt.Sprintf("Revoke member: proton drive photos albums share remove %s %s", albumID, invitee),
		"drive", "photos", "albums", "share", "remove", "--", albumID, invitee)

	status := runOK(t, "drive", "photos", "albums", "share", "get", "--", albumID)
	assertContains(t, status, invitee)
	assertContains(t, status, "not yet accepted")
	assertContains(t, status, "album")

	runOK(t, "drive", "photos", "albums", "share", "update", "--access", "editor", "--", albumID, invitee)
	assertContains(t, runOK(t, "drive", "photos", "albums", "share", "get", "--", albumID), "editor")

	runOK(t, "drive", "photos", "albums", "share", "remove", "--", albumID, invitee)
	after := runOKBothStreams(t, "drive", "photos", "albums", "share", "get", "--", albumID)
	assertNotContains(t, after, invitee)
}

// TestDriveAlbumSharedWithAnotherAccount follows an album the whole way: the
// second account is invited, accepts, and reads the photos the album holds.
func TestDriveAlbumSharedWithAnotherAccount(t *testing.T) {
	invitee := secondaryEmail()
	albumName := testID() + "-album-rt"
	albumID := createdAlbum(t, albumName)
	runOK(t, "drive", "photos", "albums", "add", albumID, uploadedPhoto(t))

	before := altInvitationIDs(t)
	runOK(t, "drive", "photos", "albums", "share", "add", "--access", "editor", "--", albumID, invitee)
	cleanupRun(t, fmt.Sprintf("Revoke member: proton drive photos albums share remove %s %s", albumID, invitee),
		"drive", "photos", "albums", "share", "remove", "--", albumID, invitee)

	// What is on offer says which thing it is, which is the whole use of a
	// listing of invitations.
	var offer map[string]interface{}
	waitFor(45*time.Second, 3*time.Second, func() bool {
		for _, i := range runJSONArraySecondary(t, "drive", "invitations", "list") {
			m := i.(map[string]interface{})
			if id, _ := m["invitation_id"].(string); !before[id] {
				offer = m
				return true
			}
		}
		return false
	})
	if offer == nil {
		t.Fatal("the second account never saw the album invitation")
	}
	if got, _ := offer["type"].(string); got != "album" {
		t.Errorf("invitation type: got %q want album", got)
	}
	if got, _ := offer["name"].(string); got != albumName {
		t.Errorf("invitation name: got %q want %q", got, albumName)
	}
	runOKSecondary(t, "drive", "invitations", "accept", offer["invitation_id"].(string))

	var shared map[string]interface{}
	waitFor(45*time.Second, 3*time.Second, func() bool {
		for _, it := range runJSONArraySecondary(t, "drive", "shared", "list") {
			m := it.(map[string]interface{})
			if name, _ := m["name"].(string); name == albumName {
				shared = m
				return true
			}
		}
		return false
	})
	if shared == nil {
		t.Fatal("the album never appeared in what the second account has been shared")
	}
	if got, _ := shared["type"].(string); got != "album" {
		t.Errorf("shared album type: got %q want album", got)
	}

	// The album opens like anything else shared, and the photos in it are what
	// is inside it.
	ref := shared["link_id"].(string)
	inside := runJSONArraySecondary(t, "drive", "items", "list", "--shared", ref, "/")
	if len(inside) == 0 {
		t.Fatal("the shared album lists none of its photos")
	}
	name, _ := inside[0].(map[string]interface{})["name"].(string)
	if name == "" {
		t.Fatal("the shared album's photo has no readable name")
	}
	out := filepath.Join(t.TempDir(), "shared-photo")
	runOKSecondary(t, "drive", "items", "download", "--shared", ref, "--dest", out, "/"+name)
	if fi, err := os.Stat(out); err != nil || fi.Size() == 0 {
		t.Errorf("a photo out of the shared album came back empty: %v", err)
	}
}

// A photo is handed to somebody by address the way anything else is.
func TestDrivePhotoSharedWithAnAddress(t *testing.T) {
	invitee := secondaryEmail()
	photoID := uploadedPhoto(t)

	runOK(t, "drive", "photos", "share", "add", "--", photoID, invitee)
	cleanupRun(t, fmt.Sprintf("Revoke member: proton drive photos share remove %s %s", photoID, invitee),
		"drive", "photos", "share", "remove", "--", photoID, invitee)

	status := runOK(t, "drive", "photos", "share", "get", "--", photoID)
	assertContains(t, status, invitee)
	assertContains(t, status, "not yet accepted")

	runOK(t, "drive", "photos", "share", "remove", "--", photoID, invitee)
	assertNotContains(t, runOKBothStreams(t, "drive", "photos", "share", "get", "--", photoID), invitee)
}

// A photo carries a public link, and the URL it hands back is the one to send.
func TestDrivePhotoLinkLifecycle(t *testing.T) {
	photoID := uploadedPhoto(t)

	url := strings.TrimSpace(runOK(t, "drive", "photos", "links", "create", "--", photoID))
	if !strings.Contains(url, "/urls/") {
		t.Fatalf("photo link stdout has no public URL: %q", url)
	}
	if !strings.Contains(url, "#") {
		t.Errorf("the photo's link is missing the password fragment: %q", url)
	}
	assertContains(t, runOK(t, "drive", "photos", "links", "get", "--", photoID), tokenOf(t, url))

	runOK(t, "drive", "photos", "links", "revoke", "--", photoID)
	after := runOKBothStreams(t, "drive", "photos", "share", "get", "--", photoID)
	assertField(t, after, "Shared:", "no")
}

// An album has no public link, so the command that would make one refuses
// before anything is created.
func TestDrivePhotoLinkRefusesAnAlbum(t *testing.T) {
	albumID := createdAlbum(t, testID()+"-album-nolink")

	_, stderr, code := run(t, "drive", "photos", "links", "create", "--", albumID)
	if code == 0 {
		t.Error("a public link for an album should be refused")
	}
	assertContains(t, stderr, "is an album")
}
