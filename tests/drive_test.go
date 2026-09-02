package tests

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// ── list ──

// A table draws no header when it has no rows, so the listing needs something
// to list rather than whatever another test happened to leave behind.
func TestDriveItemsList(t *testing.T) {
	t.Parallel()
	folder := "/" + testID() + "-list"
	runOK(t, "drive", "items", "create", folder)
	cleanupRun(t, fmt.Sprintf("Delete folder: proton drive items delete %s", folder),
		"drive", "items", "delete", folder)

	stdout := runOK(t, "drive", "items", "list")
	assertContains(t, stdout, "NAME")
	assertContains(t, stdout, strings.TrimPrefix(folder, "/"))
}

func TestDriveItemsListJSONFieldNames(t *testing.T) {
	t.Parallel()
	data := runJSONArray(t, "drive", "items", "list")
	if len(data) == 0 {
		t.Skip("drive root is empty")
	}
	item := data[0].(map[string]interface{})
	for _, field := range []string{"link_id", "name", "type", "size"} {
		if _, ok := item[field]; !ok {
			t.Errorf("expected json field %q, got keys: %v", field, keysOf(item))
		}
	}
}

// ── info ──

func TestDriveItemsInfo(t *testing.T) {
	t.Parallel()
	folder := "/" + testID() + "-info"
	tmp := t.TempDir()
	src := filepath.Join(tmp, "doc.txt")
	_ = os.WriteFile(src, []byte("info-payload-12345"), 0644)

	runOK(t, "drive", "items", "create", folder)
	cleanupRun(t, fmt.Sprintf("Delete folder: proton drive items delete --permanent %s", folder),
		"drive", "items", "delete", folder)
	runOK(t, "drive", "items", "upload", src, folder)

	info := runJSON(t, "drive", "items", "get", folder+"/doc.txt")
	if info["type"] != "file" {
		t.Errorf("type = %v, want file", info["type"])
	}
	if sz, _ := info["size_bytes"].(float64); sz <= 0 {
		t.Errorf("size_bytes = %v, want > 0", info["size_bytes"])
	}
	if _, ok := info["mime_type"]; !ok {
		t.Errorf("expected mime_type, got keys %v", keysOf(info))
	}
	if info["shared"] != false {
		t.Errorf("shared = %v, want false", info["shared"])
	}
	if info["signature"] != "verified" {
		t.Errorf("signature = %v, want verified (we uploaded it)", info["signature"])
	}

	// Text mode renders the key/value block.
	text := runOK(t, "drive", "items", "get", folder+"/doc.txt")
	assertContains(t, text, "Type:")
	assertContains(t, text, "Size:")
}

func TestDriveItemsInfoFolder(t *testing.T) {
	t.Parallel()
	folder := "/" + testID() + "-infodir"
	runOK(t, "drive", "items", "create", folder)
	cleanupRun(t, fmt.Sprintf("Delete folder: proton drive items delete --permanent %s", folder),
		"drive", "items", "delete", folder)

	info := runJSON(t, "drive", "items", "get", folder)
	if info["type"] != "folder" {
		t.Errorf("type = %v, want folder", info["type"])
	}
}

// ── upload / download lifecycle ──

func TestDriveItemsUploadDownload(t *testing.T) {
	t.Parallel()
	folder := "/" + testID() + "-upload"
	tmp := t.TempDir()
	src := filepath.Join(tmp, "payload.txt")
	want := "hello from drive test"
	_ = os.WriteFile(src, []byte(want), 0644)

	runOK(t, "drive", "items", "create", folder)
	cleanupRun(t, fmt.Sprintf("Delete folder: proton drive items delete --permanent %s", folder),
		"drive", "items", "delete", folder)

	runOK(t, "drive", "items", "upload", src, folder)
	out := filepath.Join(tmp, "out.txt")
	runOK(t, "drive", "items", "download", folder+"/payload.txt", "--dest", out)
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("downloaded file not readable: %v", err)
	}
	if string(data) != want {
		t.Errorf("content mismatch: got %q, want %q", string(data), want)
	}
}

func TestDriveItemsUploadFromStdin(t *testing.T) {
	t.Parallel()
	folder := "/" + testID() + "-stdin"
	runOK(t, "drive", "items", "create", folder)
	cleanupRun(t, fmt.Sprintf("Delete folder: proton drive items delete --permanent %s", folder),
		"drive", "items", "delete", folder)

	payload := []byte("piped payload\n")
	if _, stderr, code := runWithStdin(t, bytes.NewReader(payload),
		"drive", "items", "upload", "-", folder); code != 0 {
		t.Fatalf("stdin upload failed (exit %d): %s", code, truncateOutput(stderr))
	}

	// Find the uploaded file (name is stdin-<ts>)
	children := runJSONArray(t, "drive", "items", "list", folder)
	if len(children) != 1 {
		t.Fatalf("expected 1 child after stdin upload, got %d", len(children))
	}
	name := children[0].(map[string]interface{})["name"].(string)
	if !strings.HasPrefix(name, "stdin-") {
		t.Errorf("expected name to start with stdin-, got %q", name)
	}

	// Download back via explicit "-" (stdout capture)
	stdout := runOK(t, "drive", "items", "download", folder+"/"+name, "--dest", "-")
	if !strings.Contains(stdout, "piped payload") {
		t.Errorf("stdout download mismatch: %q", stdout)
	}
}

// A stdin DEST whose last segment isn't an existing folder names the new file:
// the basename becomes the file name and its parent is the target folder.
func TestDriveItemsUploadFromStdinNamed(t *testing.T) {
	t.Parallel()
	folder := "/" + testID() + "-stdin-named"
	runOK(t, "drive", "items", "create", folder)
	cleanupRun(t, fmt.Sprintf("Delete folder: proton drive items delete --permanent %s", folder),
		"drive", "items", "delete", folder)

	dest := folder + "/piped.txt"
	payload := "named piped payload\n"
	if _, stderr, code := runWithStdin(t, strings.NewReader(payload), "drive", "items", "upload", "-", dest); code != 0 {
		t.Fatalf("named stdin upload failed (exit %d): %s", code, stderr)
	}

	children := runJSONArray(t, "drive", "items", "list", folder)
	if len(children) != 1 {
		t.Fatalf("expected 1 child after named stdin upload, got %d", len(children))
	}
	if name := children[0].(map[string]interface{})["name"].(string); name != "piped.txt" {
		t.Errorf("expected name %q, got %q", "piped.txt", name)
	}

	stdout := runOK(t, "drive", "items", "download", dest, "--dest", "-")
	if !strings.Contains(stdout, payload) {
		t.Errorf("stdout download mismatch: %q", stdout)
	}
}

// A stdin upload under a non-existent parent fails as not-found (exit 3) and
// names the missing folder segment, never the intended filename.
func TestDriveItemsUploadFromStdinMissingParent(t *testing.T) {
	t.Parallel()
	missing := testID() + "-nope"
	_, stderr, code := runWithStdin(t, strings.NewReader("x\n"),
		"drive", "items", "upload", "-", "/"+missing+"/note.txt")
	if code != 3 {
		t.Fatalf("expected exit 3 for missing parent, got %d (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stderr, missing) {
		t.Errorf("expected error to name the missing folder %q, got: %s", missing, stderr)
	}
	if strings.Contains(stderr, "note.txt") {
		t.Errorf("error should not name the filename segment, got: %s", stderr)
	}
}

// TestDriveItemsDownloadBehaviors exercises the three download-destination
// behaviors (refuse-on-collision, --force overwrite, stdout default) against a
// single folder + uploads created once, rather than one folder+upload per
// behavior. Subtests keep per-behavior reporting.
func TestDriveItemsDownloadBehaviors(t *testing.T) {
	t.Parallel()
	folder := "/" + testID() + "-dl"
	tmp := t.TempDir()
	aSrc := filepath.Join(tmp, "a.txt")
	pSrc := filepath.Join(tmp, "p.txt")
	if err := os.WriteFile(aSrc, []byte("cloud-content"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pSrc, []byte("stdoutpayload"), 0644); err != nil {
		t.Fatal(err)
	}
	runOK(t, "drive", "items", "create", folder)
	cleanupRun(t, fmt.Sprintf("Delete folder: proton drive items delete --permanent %s", folder),
		"drive", "items", "delete", folder)
	runOK(t, "drive", "items", "upload", aSrc, folder)
	runOK(t, "drive", "items", "upload", pSrc, folder)

	t.Run("overwrite refused without --force", func(t *testing.T) {
		dest := filepath.Join(t.TempDir(), "out.txt")
		if err := os.WriteFile(dest, []byte("local-existing"), 0644); err != nil {
			t.Fatalf("setup: %v", err)
		}
		_, stderr, code := run(t, "drive", "items", "download", folder+"/a.txt", "--dest", dest)
		if code == 0 {
			t.Error("expected non-zero exit when destination exists without --force")
		}
		if !strings.Contains(stderr, "exists") {
			t.Errorf("expected stderr to mention 'exists', got: %s", stderr)
		}
		if data, _ := os.ReadFile(dest); string(data) != "local-existing" {
			t.Errorf("local file should be untouched, got: %q", string(data))
		}
	})

	t.Run("--force overwrites", func(t *testing.T) {
		dest := filepath.Join(t.TempDir(), "out.txt")
		if err := os.WriteFile(dest, []byte("local-existing"), 0644); err != nil {
			t.Fatalf("setup: %v", err)
		}
		runOK(t, "drive", "items", "download", "--force", folder+"/a.txt", "--dest", dest)
		data, err := os.ReadFile(dest)
		if err != nil {
			t.Fatalf("read after force: %v", err)
		}
		if string(data) != "cloud-content" {
			t.Errorf("--force did not overwrite, got: %q", string(data))
		}
	})

	t.Run("--dest - writes to stdout", func(t *testing.T) {
		stdout := runOK(t, "drive", "items", "download", folder+"/p.txt", "--dest", "-")
		assertContains(t, stdout, "stdoutpayload")
	})
}

func TestDriveItemsUploadRecursive(t *testing.T) {
	t.Parallel()
	folder := "/" + testID() + "-rec"
	tmp := t.TempDir()
	tree := filepath.Join(tmp, "tree")
	_ = os.MkdirAll(filepath.Join(tree, "sub1"), 0755)
	_ = os.MkdirAll(filepath.Join(tree, "sub2", "deep"), 0755)
	_ = os.WriteFile(filepath.Join(tree, "a.txt"), []byte("A"), 0644)
	_ = os.WriteFile(filepath.Join(tree, "sub1", "b.txt"), []byte("B"), 0644)
	_ = os.WriteFile(filepath.Join(tree, "sub2", "deep", "d.txt"), []byte("D"), 0644)

	runOK(t, "drive", "items", "create", folder)
	cleanupRun(t, fmt.Sprintf("Delete folder: proton drive items delete --permanent %s", folder),
		"drive", "items", "delete", folder)

	runOK(t, "drive", "items", "upload", "--recursive", tree, folder)

	top := runJSONArray(t, "drive", "items", "list", folder+"/tree")
	names := map[string]bool{}
	for _, c := range top {
		names[c.(map[string]interface{})["name"].(string)] = true
	}
	for _, want := range []string{"a.txt", "sub1", "sub2"} {
		if !names[want] {
			t.Errorf("expected %q in tree/, got %v", want, names)
		}
	}
	deep := runJSONArray(t, "drive", "items", "list", folder+"/tree/sub2/deep")
	if len(deep) != 1 || deep[0].(map[string]interface{})["name"].(string) != "d.txt" {
		t.Errorf("expected d.txt in tree/sub2/deep, got %v", deep)
	}
}

func TestDriveItemsUploadMultiBlock(t *testing.T) {
	t.Parallel()
	folder := "/" + testID() + "-big"
	tmp := t.TempDir()
	src := filepath.Join(tmp, "big.bin")
	big := make([]byte, 8*1024*1024) // 8 MB → two 4 MB blocks
	if _, err := io.ReadFull(rand.Reader, big); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(src, big, 0644)
	hWant := sha256.Sum256(big)

	runOK(t, "drive", "items", "create", folder)
	cleanupRun(t, fmt.Sprintf("Delete folder: proton drive items delete --permanent %s", folder),
		"drive", "items", "delete", folder)

	runOK(t, "drive", "items", "upload", src, folder)
	out := filepath.Join(tmp, "out.bin")
	runOK(t, "drive", "items", "download", folder+"/big.bin", "--dest", out)

	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	hGot := sha256.Sum256(got)
	if hex.EncodeToString(hGot[:]) != hex.EncodeToString(hWant[:]) {
		t.Errorf("sha256 mismatch after multi-block round-trip")
	}
}

// TestDriveItemsUploadManyBlocks crosses the upload link-batch boundary (11
// blocks > the 10-per-batch request size) so the streaming uploader has to
// request links in multiple batches and upload blocks in parallel; the sha256
// round-trip proves block ordering survives the concurrency.
func TestDriveItemsUploadManyBlocks(t *testing.T) {
	t.Parallel()
	folder := "/" + testID() + "-many"
	tmp := t.TempDir()
	src := filepath.Join(tmp, "many.bin")
	big := make([]byte, 11*4*1024*1024) // 44 MiB -> 11 x 4 MiB blocks
	if _, err := io.ReadFull(rand.Reader, big); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(src, big, 0644)
	hWant := sha256.Sum256(big)

	runOK(t, "drive", "items", "create", folder)
	cleanupRun(t, fmt.Sprintf("Delete folder: proton drive items delete --permanent %s", folder),
		"drive", "items", "delete", folder)

	runOK(t, "drive", "items", "upload", src, folder)
	out := filepath.Join(tmp, "out.bin")
	runOK(t, "drive", "items", "download", folder+"/many.bin", "--dest", out)

	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	hGot := sha256.Sum256(got)
	if hex.EncodeToString(hGot[:]) != hex.EncodeToString(hWant[:]) {
		t.Errorf("sha256 mismatch after many-block round-trip")
	}
}

// ── rename / move (re-encryption) ──

func TestDriveItemsRename(t *testing.T) {
	t.Parallel()
	folder := "/" + testID() + "-rn"
	tmp := t.TempDir()
	_ = os.WriteFile(filepath.Join(tmp, "orig.txt"), []byte("renameme"), 0644)
	runOK(t, "drive", "items", "create", folder)
	cleanupRun(t, fmt.Sprintf("Delete folder: proton drive items delete --permanent %s", folder),
		"drive", "items", "delete", folder)
	runOK(t, "drive", "items", "upload", filepath.Join(tmp, "orig.txt"), folder)

	runOK(t, "drive", "items", "update", "--name", "new.txt", folder+"/orig.txt")

	children := runJSONArray(t, "drive", "items", "list", folder)
	found := false
	for _, c := range children {
		if c.(map[string]interface{})["name"].(string) == "new.txt" {
			found = true
		}
	}
	if !found {
		t.Error("expected new.txt after rename")
	}

	// Decryption round-trip after rename
	out := filepath.Join(tmp, "after.txt")
	runOK(t, "drive", "items", "download", folder+"/new.txt", "--dest", out)
	if b, _ := os.ReadFile(out); string(b) != "renameme" {
		t.Errorf("content mismatch after rename: %q", string(b))
	}
}

func TestDriveItemsMove(t *testing.T) {
	t.Parallel()
	src := "/" + testID() + "-src"
	dst := "/" + testID() + "-dst"
	tmp := t.TempDir()
	_ = os.WriteFile(filepath.Join(tmp, "f.txt"), []byte("moveme"), 0644)

	runOK(t, "drive", "items", "create", src)
	runOK(t, "drive", "items", "create", dst)
	cleanupRun(t, fmt.Sprintf("Delete src: proton drive items delete --permanent %s", src),
		"drive", "items", "delete", src)
	cleanupRun(t, fmt.Sprintf("Delete dst: proton drive items delete --permanent %s", dst),
		"drive", "items", "delete", dst)
	runOK(t, "drive", "items", "upload", filepath.Join(tmp, "f.txt"), src)

	runOK(t, "drive", "items", "move", "--into", dst, src+"/f.txt")

	children := runJSONArray(t, "drive", "items", "list", dst)
	found := false
	for _, c := range children {
		if c.(map[string]interface{})["name"].(string) == "f.txt" {
			found = true
		}
	}
	if !found {
		t.Error("expected f.txt in dst after move")
	}

	// Re-encryption round-trip after move
	out := filepath.Join(tmp, "after.txt")
	runOK(t, "drive", "items", "download", dst+"/f.txt", "--dest", out)
	if b, _ := os.ReadFile(out); string(b) != "moveme" {
		t.Errorf("content mismatch after move: %q", string(b))
	}
}

// ── delete + trash ──

func TestDriveItemsDeleteAndTrashRestore(t *testing.T) {
	t.Parallel()
	lease(t, driveTrash)
	folder := "/" + testID() + "-trash"
	runOK(t, "drive", "items", "create", folder)
	cleanupRun(t, fmt.Sprintf("Final delete: proton drive items delete --permanent %s", folder),
		"drive", "items", "delete", folder)

	// Noted before the trash: a trashed item has no path any more, so its own ID
	// is what identifies it. Picking the first folder in the trash instead would
	// restore whatever else happened to be in there.
	linkID, _ := runJSON(t, "drive", "items", "get", folder)["link_id"].(string)
	if linkID == "" {
		t.Fatal("drive items get should report the folder's link ID")
	}

	// Non-permanent → trash
	runOK(t, "drive", "items", "trash", folder)

	// Should appear in trash, by name and with the moment it was trashed: the
	// listing is what somebody decides from, so it says what each item is.
	var entry map[string]interface{}
	for _, e := range runJSONArray(t, "drive", "trash", "list") {
		if row, ok := e.(map[string]interface{}); ok && row["link_id"] == linkID {
			entry = row
		}
	}
	if entry == nil {
		t.Fatal("the trashed folder should appear in the trash")
	}
	if name, _ := entry["name"].(string); name != strings.TrimPrefix(folder, "/") {
		t.Errorf("trash list reports name %q, want %q", name, strings.TrimPrefix(folder, "/"))
	}
	if trashed, _ := entry["trashed"].(float64); trashed <= 0 {
		t.Errorf("trash list reports trashed %v, want the moment it was trashed", entry["trashed"])
	}

	// A trashed item has no path, so it is restored by the ID the listing showed.
	runOK(t, "drive", "trash", "restore", "--", linkID)

	// It should be back in root
	top := runJSONArray(t, "drive", "items", "list")
	back := false
	folderName := strings.TrimPrefix(folder, "/")
	for _, c := range top {
		if c.(map[string]interface{})["name"].(string) == folderName {
			back = true
		}
	}
	if !back {
		t.Error("restored folder should be back in root")
	}
}

// The three answers to a name that is taken, and the refusal when none is given.
// Each is its own upload path, and replacing is the only way this CLI can make a
// file have a history at all.
func TestDriveItemsUploadRefusesADuplicate(t *testing.T) {
	t.Parallel()
	folder, src := uploadedTwice(t, "first")

	stdout, stderr, code := run(t, "drive", "items", "upload", src, folder)
	if code != 4 {
		t.Fatalf("a taken name should exit 4 (conflict), got %d: %s%s", code, stdout, stderr)
	}
	assertContains(t, stderr, "already has a file")
	for _, answer := range []string{"replace", "rename", "skip"} {
		assertContains(t, stderr, "--if-exists "+answer)
	}
}

func TestDriveItemsUploadIfExistsSkipLeavesItAlone(t *testing.T) {
	t.Parallel()
	folder, src := uploadedTwice(t, "first")
	if err := os.WriteFile(src, []byte("second"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, stderr := runOKStderr(t, "drive", "items", "upload", "--if-exists", "skip", src, folder)

	assertContains(t, stderr, "Nothing to upload")
	assertReads(t, folder+"/note.txt", "first")
}

func TestDriveItemsUploadIfExistsRenameKeepsBoth(t *testing.T) {
	t.Parallel()
	folder, src := uploadedTwice(t, "first")
	if err := os.WriteFile(src, []byte("second"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, stderr := runOKStderr(t, "drive", "items", "upload", "--if-exists", "rename", src, folder)

	// The number goes before the extension, in brackets, after a space - the same
	// name Proton's own client gives the copy it keeps.
	assertContains(t, stderr, "note (1).txt")
	assertReads(t, folder+"/note.txt", "first")
	assertReads(t, folder+"/note (1).txt", "second")
}

func TestDriveItemsUploadIfExistsReplaceKeepsHistory(t *testing.T) {
	t.Parallel()
	folder, src := uploadedTwice(t, "first")
	if err := os.WriteFile(src, []byte("second"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, stderr := runOKStderr(t, "drive", "items", "upload", "--if-exists", "replace", src, folder)
	assertContains(t, stderr, "as a new revision")
	assertReads(t, folder+"/note.txt", "second")

	revs := runJSONArray(t, "drive", "items", "revisions", "list", folder+"/note.txt")
	if len(revs) != 2 {
		t.Fatalf("replacing should leave the earlier revision, got %d", len(revs))
	}
	var earlier string
	for _, row := range revs {
		r := row.(map[string]interface{})
		if state, _ := r["state"].(float64); int(state) != 1 {
			earlier, _ = r["id"].(string)
		}
	}
	if earlier == "" {
		t.Fatalf("no superseded revision among %v", revs)
	}

	runOK(t, "drive", "items", "revisions", "restore", folder+"/note.txt", earlier)
	// Proton answers a restore with "accepted" and carries it out in the
	// background, so the earlier contents come back a moment later.
	waitFor(30*time.Second, 2*time.Second, func() bool {
		return reads(t, folder+"/note.txt") == "first"
	})
	assertReads(t, folder+"/note.txt", "first")
}

// Wanting an old version's bytes is not wanting them back in place, so reading
// one leaves the file alone.
func TestDriveRevisionsDownloadLeavesTheFileAlone(t *testing.T) {
	t.Parallel()
	folder, path, earlier := fileWithHistory(t)

	out := filepath.Join(t.TempDir(), "earlier.txt")
	_, stderr := runOKStderr(t, "drive", "items", "revisions", "download", "--dest", out, path, earlier)

	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "first" {
		t.Errorf("the earlier revision reads %q, want %q", got, "first")
	}
	// The date is what tells two versions of one file apart, so the confirmation
	// says which one arrived rather than only the name they share.
	assertContains(t, stderr, "as it was on")
	assertReads(t, folder+"/note.txt", "second")
}

func TestDriveRevisionsDeleteDropsOneVersion(t *testing.T) {
	t.Parallel()
	_, path, earlier := fileWithHistory(t)

	runOK(t, "drive", "items", "revisions", "delete", path, earlier)

	revs := runJSONArray(t, "drive", "items", "revisions", "list", path)
	if len(revs) != 1 {
		t.Fatalf("deleting one of two revisions left %d", len(revs))
	}
	assertReads(t, path, "second")
}

// The current version is what the file is made of, so neither command that acts
// on history will touch it: restoring it is nothing, and deleting it is deleting
// the file, which is a different command.
func TestDriveRevisionsRefuseTheCurrentVersion(t *testing.T) {
	t.Parallel()
	_, path, _ := fileWithHistory(t)
	current := currentRevision(t, path)

	_, stderr, code := run(t, "--yes", "drive", "items", "revisions", "delete", path, current)
	if code == 0 {
		t.Errorf("deleting the current version was allowed")
	}
	assertContains(t, stderr, "current version")

	_, stderr, code = run(t, "--yes", "drive", "items", "revisions", "restore", path, current)
	if code == 0 {
		t.Errorf("restoring the current version was allowed")
	}
	assertContains(t, stderr, "already at")
}

// A revision nobody can find is an unfound reference like any other, which the
// CLI answers with exit 3 rather than passing the string to Proton.
func TestDriveRevisionsUnknownReferenceExitsNotFound(t *testing.T) {
	t.Parallel()
	_, path, _ := fileWithHistory(t)

	_, _, code := run(t, "drive", "items", "revisions", "restore", path, "nosuchrevision")
	if code != 3 {
		t.Errorf("expected exit 3 for an unknown revision, got %d", code)
	}
}

// fileWithHistory makes a file with two versions, and hands back the folder, the
// file, and the ID of the version that was superseded.
func fileWithHistory(t *testing.T) (folder, path, earlier string) {
	t.Helper()
	folder, src := uploadedTwice(t, "first")
	if err := os.WriteFile(src, []byte("second"), 0o600); err != nil {
		t.Fatal(err)
	}
	runOK(t, "drive", "items", "upload", "--if-exists", "replace", src, folder)

	path = folder + "/note.txt"
	for _, row := range runJSONArray(t, "drive", "items", "revisions", "list", path) {
		r := row.(map[string]interface{})
		if state, _ := r["state"].(float64); int(state) != 1 {
			earlier, _ = r["id"].(string)
		}
	}
	if earlier == "" {
		t.Fatalf("%s has no superseded revision", path)
	}
	return folder, path, earlier
}

func currentRevision(t *testing.T, path string) string {
	t.Helper()
	for _, row := range runJSONArray(t, "drive", "items", "revisions", "list", path) {
		r := row.(map[string]interface{})
		if state, _ := r["state"].(float64); int(state) == 1 {
			id, _ := r["id"].(string)
			return id
		}
	}
	t.Fatalf("%s has no current revision", path)
	return ""
}

// uploadedTwice makes a folder holding note.txt, and hands back the folder and a
// local file of the same name to upload over it.
func uploadedTwice(t *testing.T, content string) (folder, src string) {
	t.Helper()
	folder = "/" + testID() + "-conflict"
	runOK(t, "drive", "items", "create", folder)
	cleanupRun(t, fmt.Sprintf("Delete folder: proton drive items delete --permanent %s", folder),
		"drive", "items", "delete", folder)

	src = filepath.Join(t.TempDir(), "note.txt")
	if err := os.WriteFile(src, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	runOK(t, "drive", "items", "upload", src, folder)
	return folder, src
}

// reads downloads a file and hands back what it holds.
func reads(t *testing.T, path string) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "downloaded")
	runOK(t, "drive", "items", "download", "--dest", out, path)
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	return string(got)
}

func assertReads(t *testing.T, path, want string) {
	t.Helper()
	if got := reads(t, path); got != want {
		t.Errorf("%s reads %q, want %q", path, got, want)
	}
}

// A tree is one thing with one name, so the answer is about the folder it lands
// in rather than about each file: it merges into what is there, moves aside
// whole, or is not written at all.
func TestDriveItemsUploadTreeIfExistsReplaceMerges(t *testing.T) {
	t.Parallel()
	dest, local := uploadedTree(t)
	writeLocal(t, filepath.Join(local, "a.txt"), "second")
	writeLocal(t, filepath.Join(local, "c.txt"), "new")

	_, stderr := runOKStderr(t, "drive", "items", "upload", "--recursive", "--if-exists", "replace", local, dest)

	assertContains(t, stderr, "to "+dest+"/project")
	assertReads(t, dest+"/project/a.txt", "second")
	assertReads(t, dest+"/project/c.txt", "new")
	assertReads(t, dest+"/project/sub/b.txt", "deep")
}

func TestDriveItemsUploadTreeIfExistsRenameKeepsBoth(t *testing.T) {
	t.Parallel()
	dest, local := uploadedTree(t)
	writeLocal(t, filepath.Join(local, "a.txt"), "second")

	_, stderr := runOKStderr(t, "drive", "items", "upload", "--recursive", "--if-exists", "rename", local, dest)

	assertContains(t, stderr, "project (1)")
	assertReads(t, dest+"/project/a.txt", "first")
	assertReads(t, dest+"/project (1)/a.txt", "second")
	assertReads(t, dest+"/project (1)/sub/b.txt", "deep")
}

func TestDriveItemsUploadTreeIfExistsSkipWritesNothing(t *testing.T) {
	t.Parallel()
	dest, local := uploadedTree(t)
	writeLocal(t, filepath.Join(local, "c.txt"), "new")

	_, stderr := runOKStderr(t, "drive", "items", "upload", "--recursive", "--if-exists", "skip", local, dest)

	assertContains(t, stderr, "Nothing to upload")
	if holds := childNames(t, dest+"/project"); slices.Contains(holds, "c.txt") {
		t.Errorf("skipping the tree still wrote into it: %v", holds)
	}
}

// A file and a folder cannot take each other's place, so no answer reaches this
// one - and the refusal has to come before any of the tree is written.
func TestDriveItemsUploadTreeRefusesAFileWhereAFolderGoes(t *testing.T) {
	t.Parallel()
	dest := "/" + testID() + "-mismatch"
	runOK(t, "drive", "items", "create", dest)
	cleanupRun(t, fmt.Sprintf("Delete folder: proton drive items delete --permanent %s", dest),
		"drive", "items", "delete", dest)
	runOK(t, "drive", "items", "create", dest+"/project")

	local := treeOnDisk(t)
	inTheWay := filepath.Join(t.TempDir(), "sub")
	writeLocal(t, inTheWay, "in the way")
	runOK(t, "drive", "items", "upload", inTheWay, dest+"/project")

	_, stderr, code := run(t, "drive", "items", "upload", "--recursive", "--if-exists", "replace", local, dest)

	if code != 4 {
		t.Fatalf("a file where a folder goes should exit 4 (conflict), got %d: %s", code, stderr)
	}
	assertContains(t, stderr, `already has a file called "sub"`)
	if holds := childNames(t, dest+"/project"); len(holds) != 1 || holds[0] != "sub" {
		t.Errorf("the upload wrote %v before refusing", holds)
	}
}

// uploadedTree puts a small tree in Drive and hands back the folder it went into
// and the local directory it came from.
func uploadedTree(t *testing.T) (dest, local string) {
	t.Helper()
	dest = "/" + testID() + "-tree"
	runOK(t, "drive", "items", "create", dest)
	cleanupRun(t, fmt.Sprintf("Delete folder: proton drive items delete --permanent %s", dest),
		"drive", "items", "delete", dest)
	local = treeOnDisk(t)
	runOK(t, "drive", "items", "upload", "--recursive", local, dest)
	return dest, local
}

func treeOnDisk(t *testing.T) string {
	t.Helper()
	local := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(filepath.Join(local, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeLocal(t, filepath.Join(local, "a.txt"), "first")
	writeLocal(t, filepath.Join(local, "sub", "b.txt"), "deep")
	return local
}

func writeLocal(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func childNames(t *testing.T, folder string) []string {
	t.Helper()
	var names []string
	for _, child := range runJSONArray(t, "drive", "items", "list", folder) {
		names = append(names, child.(map[string]interface{})["name"].(string))
	}
	return names
}

// Emptying the trash is the one command that acts on everything in it, so it
// takes the trash to itself: another test restoring something would find it
// permanently gone.
func TestDriveTrashEmpty(t *testing.T) {
	t.Parallel()
	lease(t, driveTrash)
	folder := "/" + testID() + "-emptytrash"
	runOK(t, "drive", "items", "create", folder)
	linkID, _ := runJSON(t, "drive", "items", "get", folder)["link_id"].(string)
	if linkID == "" {
		t.Fatal("drive items get should report the folder's link ID")
	}
	// No cleanup: emptying the trash is what removes it, and a folder left in
	// the trash by a failure is swept by the next seed.
	runOK(t, "drive", "items", "trash", folder)

	if !trashHolds(t, linkID) {
		t.Fatal("the trashed folder should be in the trash")
	}
	runOK(t, "drive", "trash", "empty")
	if trashHolds(t, linkID) {
		t.Error("the trash still holds the folder after being emptied")
	}
}

func trashHolds(t *testing.T, linkID string) bool {
	t.Helper()
	for _, row := range runJSONArray(t, "drive", "trash", "list") {
		if row.(map[string]interface{})["link_id"] == linkID {
			return true
		}
	}
	return false
}

// ── batch filters (all dry-run) ──

func TestDriveBatchDeletePatternDryRun(t *testing.T) {
	t.Parallel()
	folder := "/" + testID() + "-pat"
	tmp := t.TempDir()
	for _, n := range []string{"a.log", "b.log", "keep.txt"} {
		_ = os.WriteFile(filepath.Join(tmp, n), []byte("x"), 0644)
	}
	runOK(t, "drive", "items", "create", folder)
	cleanupRun(t, fmt.Sprintf("Delete folder: proton drive items delete --permanent %s", folder),
		"drive", "items", "delete", folder)
	for _, n := range []string{"a.log", "b.log", "keep.txt"} {
		runOK(t, "drive", "items", "upload", filepath.Join(tmp, n), folder)
	}

	_, stderr := runOKStderr(t, "--dry-run", "drive", "items", "delete",
		"--pattern", "*.log", "--scope", folder, "--recursive")
	assertContains(t, stderr, "would delete 2 items")
	assertContains(t, stderr, "a.log")
	assertContains(t, stderr, "b.log")
	assertNotContains(t, stderr, "keep.txt")
}

// Deleting is permanent, so it is confirmed rather than assumed - and never more
// so than with --all, which covers the whole drive. A test is not a terminal, so
// the only way through is --yes, and its absence has to stop the command rather
// than hang it.
//
// This one runs without --yes on purpose. Nothing else in the suite does, and if
// the guard ever stopped working this is the test standing between a stray --all
// and the account's entire Drive.
func TestDriveBatchDeleteAllNeedsConfirming(t *testing.T) {
	t.Parallel()
	_, stderr, code := run(t, "drive", "items", "delete", "--all")
	if code != 1 {
		t.Fatalf("--all alone must be stopped for confirmation, got exit %d: %s", code, stderr)
	}
	assertContains(t, stderr, "cannot be undone")
	assertContains(t, stderr, "--yes")
	assertContains(t, stderr, "--dry-run")
}

// ── folders ──

func TestDriveFoldersCreate(t *testing.T) {
	t.Parallel()
	folder := "/" + testID() + "-folder"
	runOK(t, "drive", "items", "create", folder)
	cleanupRun(t, fmt.Sprintf("Delete folder: proton drive items delete --permanent %s", folder),
		"drive", "items", "delete", folder)

	top := runJSONArray(t, "drive", "items", "list")
	name := strings.TrimPrefix(folder, "/")
	found := false
	for _, c := range top {
		if c.(map[string]interface{})["name"].(string) == name {
			found = true
		}
	}
	if !found {
		t.Errorf("folder %s not in root listing", folder)
	}
}

// A path names every folder along it, so naming one that is three deep makes
// three, and says so rather than reporting the one that was typed.
func TestDriveFoldersCreateMakesTheFoldersAboveIt(t *testing.T) {
	t.Parallel()
	top := "/" + testID() + "-nested"
	deep := top + "/season/episode"
	_, stderr := runOKStderr(t, "drive", "items", "create", deep)
	cleanupRun(t, fmt.Sprintf("Delete folder: proton drive items delete %s", top),
		"drive", "items", "delete", top)

	assertContains(t, stderr, "Created 3 folders down to "+deep)
	assertContains(t, runOK(t, "drive", "items", "list", top+"/season"), "episode")
}

// The folder asked for is the one thing the command does not make twice: it is
// what was named, and a name Drive already holds is a conflict, not a no-op.
func TestDriveFoldersCreateRefusesANameAlreadyThere(t *testing.T) {
	t.Parallel()
	folder := "/" + testID() + "-taken"
	runOK(t, "drive", "items", "create", folder)
	cleanupRun(t, fmt.Sprintf("Delete folder: proton drive items delete %s", folder),
		"drive", "items", "delete", folder)

	_, stderr, code := run(t, "drive", "items", "create", folder)
	if code != 4 {
		t.Fatalf("want exit 4 for a name already taken, got %d (stderr: %s)", code, stderr)
	}
	assertContains(t, stderr, "already has a folder")
}

func TestDriveItemsCopy(t *testing.T) {
	t.Parallel()
	base := "/" + testID() + "-copy-src"
	dest := "/" + testID() + "-copy-dst"
	runOK(t, "drive", "items", "create", base)
	cleanupRun(t, fmt.Sprintf("Delete folder: proton drive items delete --permanent %s", base),
		"drive", "items", "delete", base)
	runOK(t, "drive", "items", "create", dest)
	cleanupRun(t, fmt.Sprintf("Delete folder: proton drive items delete --permanent %s", dest),
		"drive", "items", "delete", dest)

	dir := t.TempDir()
	src := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(src, []byte("copy me"), 0o600); err != nil {
		t.Fatal(err)
	}
	runOK(t, "drive", "items", "upload", src, base)

	runOK(t, "drive", "items", "copy", "--into", dest, base+"/f.txt")
	assertContains(t, runOK(t, "drive", "items", "list", dest), "f.txt")

	out := filepath.Join(dir, "out.txt")
	runOK(t, "drive", "items", "download", dest+"/f.txt", "--dest", out)
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read copied file: %v", err)
	}
	if string(got) != "copy me" {
		t.Errorf("copy content mismatch: got %q want %q", got, "copy me")
	}
}

func TestDriveItemsRevisions(t *testing.T) {
	t.Parallel()
	folder := "/" + testID() + "-rev"
	runOK(t, "drive", "items", "create", folder)
	cleanupRun(t, fmt.Sprintf("Delete folder: proton drive items delete --permanent %s", folder),
		"drive", "items", "delete", folder)

	dir := t.TempDir()
	src := filepath.Join(dir, "r.txt")
	if err := os.WriteFile(src, []byte("v1"), 0o600); err != nil {
		t.Fatal(err)
	}
	runOK(t, "drive", "items", "upload", src, folder)

	out := runOK(t, "drive", "items", "revisions", "list", folder+"/r.txt")
	assertContains(t, out, "active")
}

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
	t.Parallel()
	lease(t, photos)
	if _, stderr, code := run(t, "drive", "photos", "list"); code != 0 {
		if strings.Contains(stderr, "photos") {
			t.Skip("no photos share on this account")
		}
		t.Fatalf("photos list failed: %s", truncateOutput(stderr))
	}

	before := photoLinkIDs(t)

	dir := t.TempDir()
	img := filepath.Join(dir, testID()+".png")
	writePNG(t, img)
	runOK(t, "drive", "photos", "upload", img)

	// Identify the uploaded photo as the new entry in the listing.
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

	// Download round-trip via the unified model: explicit file, then --dest-dir.
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

// TestDrivePhotosTrash covers the D3 verb split for photos: `trash` moves a
// photo to the trash (it leaves the timeline listing), and `delete` (permanent)
// cleans it up.
func TestDrivePhotosTrash(t *testing.T) {
	t.Parallel()
	lease(t, photos)
	if _, stderr, code := run(t, "drive", "photos", "list"); code != 0 {
		if strings.Contains(stderr, "photos") {
			t.Skip("no photos share on this account")
		}
		t.Fatalf("photos list failed: %s", truncateOutput(stderr))
	}
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

	runOK(t, "drive", "photos", "trash", "--", photoID)
	if !waitFor(15*time.Second, 1*time.Second, func() bool { return !photoLinkIDs(t)[photoID] }) {
		t.Errorf("trashed photo %s still appears in the timeline listing", photoID)
	}
}

func TestDrivePhotosFavoriteRoundTrip(t *testing.T) {
	t.Parallel()
	lease(t, photos)
	if _, stderr, code := run(t, "drive", "photos", "list"); code != 0 {
		if strings.Contains(stderr, "photos") {
			t.Skip("no photos share on this account")
		}
		t.Fatalf("photos list failed: %s", truncateOutput(stderr))
	}

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

func TestDrivePhotosRead(t *testing.T) {
	t.Parallel()
	_, stderr, code := run(t, "drive", "photos", "list")
	if code != 0 {
		if strings.Contains(stderr, "photos") {
			t.Skip("no photos share on this account")
		}
		t.Fatalf("photos list failed (exit %d): %s", code, truncateOutput(stderr))
	}
	// Albums listing exercises the same photos-share bootstrap + name decryption.
	runOK(t, "drive", "photos", "albums", "list")
}

// Which tag names exist is judged from the command line, so that is asserted in
// the offline suite. What is left here is that a name Proton knows actually
// filters against the library.
func TestDrivePhotosListTags(t *testing.T) {
	t.Parallel()
	if _, stderr, code := run(t, "drive", "photos", "list", "--tag", "videos"); code != 0 {
		if strings.Contains(stderr, "photos") {
			t.Skip("no photos share on this account")
		}
		t.Fatalf("list --tags videos failed: %s", truncateOutput(stderr))
	}
}

// TestDrivePhotosTagsSubcommandRemoved guards the deliberate scope decision:
// the web UI exposes no add/remove-tag action, so neither do we. favorite /
// unfavorite stay; the old `photos tags` subcommand is gone.
// Favouriting is a verb, not a tag to be set: `tags` would invite a second way to
// say the same thing.
func TestDrivePhotosFavouriteIsAVerb(t *testing.T) {
	t.Parallel()
	help := runOK(t, "drive", "photos", "--help")
	assertContains(t, help, "favorite")
	assertContains(t, help, "unfavorite")
	for _, line := range strings.Split(help, "\n") {
		if fields := strings.Fields(line); len(fields) > 0 && fields[0] == "tags" {
			t.Errorf("photos should not expose a 'tags' subcommand:\n%s", help)
		}
	}
}

// An album's cover is which of its own photos represents it, so a photo that is
// not in the album is refused rather than stored as a reference nothing resolves.
func TestDrivePhotoAlbumCover(t *testing.T) {
	t.Parallel()
	lease(t, photos)

	name := testID() + "-cover"
	albumID := strings.TrimSpace(runOK(t, "drive", "photos", "albums", "create", "--name", name))
	cleanupRun(t, fmt.Sprintf("Delete album: proton drive photos albums delete %s", albumID),
		"drive", "photos", "albums", "delete", "--", albumID)

	var photoID string
	for id := range photoLinkIDs(t) {
		photoID = id
		break
	}
	if photoID == "" {
		t.Skip("the library holds no photos to cover the album with")
	}
	runOK(t, "drive", "photos", "albums", "add", albumID, photoID)
	runOK(t, "drive", "photos", "albums", "update", "--cover", photoID, "--", albumID)

	// A photo outside the album cannot represent it.
	_, stderr, code := run(t, "drive", "photos", "albums", "update",
		"--cover", "notaphoto", "--", albumID)
	if code == 0 {
		t.Error("a cover that is not in the album should be refused")
	}
	_ = stderr
}
