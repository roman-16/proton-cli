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
	before := photoLinkIDs(t)
	img := filepath.Join(t.TempDir(), testID()+".png")
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
	return photoID
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

	// Create an album; identify it as the new entry in the listing.
	albumsBefore := map[string]bool{}
	for _, a := range runJSONArray(t, "drive", "photos", "albums", "list") {
		albumsBefore[a.(map[string]interface{})["link_id"].(string)] = true
	}
	albumName := testID() + "-album"
	runOK(t, "drive", "photos", "albums", "create", "--name", albumName)
	var albumID, albumNameSeen string
	for _, a := range runJSONArray(t, "drive", "photos", "albums", "list") {
		m := a.(map[string]interface{})
		id := m["link_id"].(string)
		if !albumsBefore[id] {
			albumID = id
			albumNameSeen, _ = m["name"].(string)
		}
	}
	if albumID == "" {
		t.Fatal("created album not found in listing")
	}
	if albumNameSeen != albumName {
		t.Errorf("album name: got %q want %q", albumNameSeen, albumName)
	}
	cleanupRun(t, fmt.Sprintf("Delete album: proton drive photos albums delete %s", albumID),
		"drive", "photos", "albums", "delete", "--", albumID)

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
