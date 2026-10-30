package live

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/roman-16/proton-cli/tests/account"
)

func TestDriveItemsList(t *testing.T) {
	folder := "/" + testID() + "-list"
	runOK(t, "drive", "items", "create", folder)
	cleanupRun(t, fmt.Sprintf("Delete folder: proton drive items delete %s", folder),
		"drive", "items", "delete", folder)

	stdout := runOK(t, "drive", "items", "list")
	assertContains(t, stdout, "NAME")
	assertContains(t, stdout, strings.TrimPrefix(folder, "/"))
}

func TestDriveItemsListJSONFieldNames(t *testing.T) {
	pinned(t, account.Primary, "drive", "Documents")
	data := runJSONArray(t, "drive", "items", "list")
	if len(data) == 0 {
		t.Fatal("the fixture pins a folder at the root and the listing is empty")
	}
	item := data[0].(map[string]interface{})
	for _, field := range []string{"link_id", "name", "type", "size"} {
		if _, ok := item[field]; !ok {
			t.Errorf("expected json field %q, got keys: %v", field, keysOf(item))
		}
	}
}

func TestDriveItemsInfo(t *testing.T) {
	folder := "/" + testID() + "-info"
	tmp := t.TempDir()
	src := filepath.Join(tmp, "doc.pdf")
	_ = os.WriteFile(src, []byte("info-payload-12345"), 0644)

	runOK(t, "drive", "items", "create", folder)
	cleanupRun(t, fmt.Sprintf("Delete folder: proton drive items delete --permanent %s", folder),
		"drive", "items", "delete", folder)
	runOK(t, "drive", "items", "upload", src, folder)

	info := runJSON(t, "drive", "items", "get", folder+"/doc.pdf")
	if info["type"] != "file" {
		t.Errorf("type = %v, want file", info["type"])
	}
	if sz, _ := info["size_bytes"].(float64); sz <= 0 {
		t.Errorf("size_bytes = %v, want > 0", info["size_bytes"])
	}
	if info["mime_type"] != "application/pdf" {
		t.Errorf("mime_type = %v, want application/pdf", info["mime_type"])
	}
	if info["shared"] != false {
		t.Errorf("shared = %v, want false", info["shared"])
	}
	if info["signature"] != "verified" {
		t.Errorf("signature = %v, want verified (we uploaded it)", info["signature"])
	}

	text := runOK(t, "drive", "items", "get", folder+"/doc.pdf")
	assertContains(t, text, "Type:")
	assertContains(t, text, "Size:")
}

func TestDriveItemsInfoFolder(t *testing.T) {
	folder := "/" + testID() + "-infodir"
	runOK(t, "drive", "items", "create", folder)
	cleanupRun(t, fmt.Sprintf("Delete folder: proton drive items delete --permanent %s", folder),
		"drive", "items", "delete", folder)

	info := runJSON(t, "drive", "items", "get", folder)
	if info["type"] != "folder" {
		t.Errorf("type = %v, want folder", info["type"])
	}
}

func TestDriveItemsUploadDownload(t *testing.T) {
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
	folder := "/" + testID() + "-stdin"
	runOK(t, "drive", "items", "create", folder)
	cleanupRun(t, fmt.Sprintf("Delete folder: proton drive items delete --permanent %s", folder),
		"drive", "items", "delete", folder)

	payload := []byte("piped payload\n")
	if _, stderr, code := runWithStdin(t, bytes.NewReader(payload),
		"drive", "items", "upload", "-", folder); code != 0 {
		t.Fatalf("stdin upload failed (exit %d): %s", code, truncateOutput(stderr))
	}

	children := runJSONArray(t, "drive", "items", "list", folder)
	if len(children) != 1 {
		t.Fatalf("expected 1 child after stdin upload, got %d", len(children))
	}
	name := children[0].(map[string]interface{})["name"].(string)
	if !strings.HasPrefix(name, "stdin-") {
		t.Errorf("expected name to start with stdin-, got %q", name)
	}

	stdout := runOK(t, "drive", "items", "download", folder+"/"+name, "--dest", "-")
	if !strings.Contains(stdout, "piped payload") {
		t.Errorf("stdout download mismatch: %q", stdout)
	}
}

func TestDriveItemsUploadFromStdinNamed(t *testing.T) {
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

func TestDriveItemsUploadFromStdinMissingParent(t *testing.T) {
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

func TestDriveItemsDownloadBehaviors(t *testing.T) {
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

func TestDriveItemsDownloadsAFolder(t *testing.T) {
	folder := "/" + testID() + "-dltree"
	tmp := t.TempDir()
	tree := filepath.Join(tmp, "tree")
	if err := os.MkdirAll(filepath.Join(tree, "sub", "deep"), 0755); err != nil {
		t.Fatal(err)
	}
	wrote := map[string]string{
		"a.txt":          "A",
		"sub/b.txt":      "B",
		"sub/deep/d.txt": "D",
	}
	for name, content := range wrote {
		if err := os.WriteFile(filepath.Join(tree, filepath.FromSlash(name)), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	runOK(t, "drive", "items", "create", folder)
	cleanupRun(t, fmt.Sprintf("Delete folder: proton drive items delete %s", folder),
		"drive", "items", "delete", folder)
	runOK(t, "drive", "items", "upload", "--recursive", tree, folder)

	_, stderr, code := run(t, "drive", "items", "download", "--dest-dir", t.TempDir(), folder+"/tree")
	if code == 0 {
		t.Error("a folder was downloaded without --recursive")
	}
	assertContains(t, stderr, "--recursive")

	into := t.TempDir()
	runOK(t, "drive", "items", "download", "--recursive", "--dest-dir", into, folder+"/tree")
	for name, want := range wrote {
		got, err := os.ReadFile(filepath.Join(into, "tree", filepath.FromSlash(name)))
		if err != nil {
			t.Errorf("%s did not land: %v", name, err)
			continue
		}
		if string(got) != want {
			t.Errorf("%s holds %q, want %q", name, got, want)
		}
	}
}

func TestDriveItemsUploadRecursive(t *testing.T) {
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
	folder := "/" + testID() + "-big"
	tmp := t.TempDir()
	src := filepath.Join(tmp, "big.bin")
	big := make([]byte, 8*1024*1024)
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

func TestDriveItemsUploadManyBlocks(t *testing.T) {
	folder := "/" + testID() + "-many"
	tmp := t.TempDir()
	src := filepath.Join(tmp, "many.bin")
	big := make([]byte, 11*4*1024*1024)
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

func TestDriveItemsRename(t *testing.T) {
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

	out := filepath.Join(tmp, "after.txt")
	runOK(t, "drive", "items", "download", folder+"/new.txt", "--dest", out)
	if b, _ := os.ReadFile(out); string(b) != "renameme" {
		t.Errorf("content mismatch after rename: %q", string(b))
	}
}

func TestDriveItemsMove(t *testing.T) {
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

	out := filepath.Join(tmp, "after.txt")
	runOK(t, "drive", "items", "download", dst+"/f.txt", "--dest", out)
	if b, _ := os.ReadFile(out); string(b) != "moveme" {
		t.Errorf("content mismatch after move: %q", string(b))
	}
}

func TestDriveItemsUploadRefusesADuplicate(t *testing.T) {
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
	folder, src := uploadedTwice(t, "first")
	if err := os.WriteFile(src, []byte("second"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, stderr := runOKStderr(t, "drive", "items", "upload", "--if-exists", "skip", src, folder)

	assertContains(t, stderr, "Nothing to upload")
	assertReads(t, folder+"/note.txt", "first")
}

func TestDriveItemsUploadIfExistsRenameKeepsBoth(t *testing.T) {
	folder, src := uploadedTwice(t, "first")
	if err := os.WriteFile(src, []byte("second"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, preview := runOKStderr(t, "drive", "items", "upload", "--if-exists", "rename", "--dry-run", src, folder)
	assertContains(t, preview, "would upload")
	assertContains(t, preview, "note (1).txt")

	_, stderr := runOKStderr(t, "drive", "items", "upload", "--if-exists", "rename", src, folder)

	assertContains(t, stderr, "note (1).txt")
	assertReads(t, folder+"/note.txt", "first")
	assertReads(t, folder+"/note (1).txt", "second")
}

func TestDriveItemsUploadIfExistsReplaceKeepsHistory(t *testing.T) {
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
	waitFor(30*time.Second, 2*time.Second, func() bool {
		return reads(t, folder+"/note.txt") == "first"
	})
	assertReads(t, folder+"/note.txt", "first")
}

func TestDriveRevisionsDownloadLeavesTheFileAlone(t *testing.T) {
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
	assertContains(t, stderr, "as it was on")
	assertReads(t, folder+"/note.txt", "second")
}

func TestDriveRevisionsDeleteDropsOneVersion(t *testing.T) {
	_, path, earlier := fileWithHistory(t)

	runOK(t, "drive", "items", "revisions", "delete", path, earlier)

	revs := runJSONArray(t, "drive", "items", "revisions", "list", path)
	if len(revs) != 1 {
		t.Fatalf("deleting one of two revisions left %d", len(revs))
	}
	assertReads(t, path, "second")
}

func TestDriveRevisionsRefuseTheCurrentVersion(t *testing.T) {
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

func TestDriveRevisionsUnknownReferenceExitsNotFound(t *testing.T) {
	_, path, _ := fileWithHistory(t)

	_, _, code := run(t, "drive", "items", "revisions", "restore", path, "nosuchrevision")
	if code != 3 {
		t.Errorf("expected exit 3 for an unknown revision, got %d", code)
	}
}

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

func TestDriveItemsUploadTreeIfExistsReplaceMerges(t *testing.T) {
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
	dest, local := uploadedTree(t)
	writeLocal(t, filepath.Join(local, "a.txt"), "second")

	_, stderr := runOKStderr(t, "drive", "items", "upload", "--recursive", "--if-exists", "rename", local, dest)

	assertContains(t, stderr, "project (1)")
	assertReads(t, dest+"/project/a.txt", "first")
	assertReads(t, dest+"/project (1)/a.txt", "second")
	assertReads(t, dest+"/project (1)/sub/b.txt", "deep")
}

func TestDriveItemsUploadTreeIfExistsSkipWritesNothing(t *testing.T) {
	dest, local := uploadedTree(t)
	writeLocal(t, filepath.Join(local, "c.txt"), "new")

	_, stderr := runOKStderr(t, "drive", "items", "upload", "--recursive", "--if-exists", "skip", local, dest)

	assertContains(t, stderr, "Nothing to upload")
	if holds := childNames(t, dest+"/project"); slices.Contains(holds, "c.txt") {
		t.Errorf("skipping the tree still wrote into it: %v", holds)
	}
}

func TestDriveItemsUploadTreeRefusesAFileWhereAFolderGoes(t *testing.T) {
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

func TestDriveBatchDeletePatternDryRun(t *testing.T) {
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

func TestDriveBatchDeleteAllNeedsConfirming(t *testing.T) {
	_, stderr, code := run(t, "drive", "items", "delete", "--all")
	if code != 1 {
		t.Fatalf("--all alone must be stopped for confirmation, got exit %d: %s", code, stderr)
	}
	assertContains(t, stderr, "cannot be undone")
	assertContains(t, stderr, "--yes")
	assertContains(t, stderr, "--dry-run")
}

func TestDriveFoldersCreate(t *testing.T) {
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

func TestDriveFoldersCreateMakesTheFoldersAboveIt(t *testing.T) {
	top := "/" + testID() + "-nested"
	deep := top + "/season/episode"
	_, stderr := runOKStderr(t, "drive", "items", "create", deep)
	cleanupRun(t, fmt.Sprintf("Delete folder: proton drive items delete %s", top),
		"drive", "items", "delete", top)

	assertContains(t, stderr, "Created 3 folders down to "+deep)
	assertContains(t, runOK(t, "drive", "items", "list", top+"/season"), "episode")
}

func TestDriveFoldersCreateRefusesANameAlreadyThere(t *testing.T) {
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

func TestDriveSettings(t *testing.T) {
	stdout := runOK(t, "drive", "settings", "get")
	assertContains(t, stdout, "Version History")
}

func TestDriveSettingsSetListsKeys(t *testing.T) {
	stdout := runOK(t, "drive", "settings", "list")
	assertContains(t, stdout, "version-history")
}

func TestDriveItemsAbuseInSomethingSharedWithYou(t *testing.T) {
	folder := "/" + testID() + "-abuse"
	runOK(t, "drive", "items", "create", folder)
	cleanupRun(t, fmt.Sprintf("Delete: proton drive items delete %s", folder),
		"drive", "items", "delete", folder)
	src := filepath.Join(t.TempDir(), "invoice.txt")
	writeLocal(t, src, "abuse-payload")
	runOK(t, "drive", "items", "upload", src, folder)

	id, _ := sharedWithSecondary(t, folder, "--access", "editor")["link_id"].(string)

	_, stderr := runOKStderrSecondary(t, "--dry-run", "drive", "items", "abuse",
		"--shared", id, "--category", "malware", "--good-faith", "/invoice.txt")
	assertContains(t, stderr, "Dry run - would report invoice.txt to Proton as malware.")

	revisions := runJSONArraySecondary(t, "drive", "items", "revisions", "list", "/invoice.txt", "--shared", id)
	if len(revisions) == 0 {
		t.Fatal("the shared file lists no revisions")
	}
	revision, _ := revisions[0].(map[string]interface{})["id"].(string)
	_, stderr = runOKStderrSecondary(t, "--dry-run", "drive", "items", "revisions", "abuse",
		"--shared", id, "--category", "copyright", "--message", "Published without permission",
		"--email", secondaryEmail(), "--good-faith", "/invoice.txt", revision)
	assertContains(t, stderr, "Dry run - would report revision")
	assertContains(t, stderr, "of invoice.txt to Proton as copyright infringement.")
}

func TestDriveItemsAbuseInALink(t *testing.T) {
	_, url := sharedLink(t, "abuselink", "link-payload")

	_, stderr := runOKStderrSecondary(t, "--dry-run", "drive", "items", "abuse",
		"--link", url, "--category", "spam", "--good-faith", "/payload.txt")

	assertContains(t, stderr, "Dry run - would report payload.txt to Proton as spam.")
}

func TestDriveItemsDownloadingAFolderAgainContinues(t *testing.T) {
	folder := "/" + testID() + "-again"
	tree := filepath.Join(t.TempDir(), "tree")
	if err := os.MkdirAll(filepath.Join(tree, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	stamped := time.Date(2026, 3, 12, 9, 41, 0, 0, time.UTC)
	for name, content := range map[string]string{"a.txt": "A", "sub/b.txt": "B"} {
		path := filepath.Join(tree, filepath.FromSlash(name))
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, stamped, stamped); err != nil {
			t.Fatal(err)
		}
	}
	runOK(t, "drive", "items", "create", folder)
	cleanupRun(t, fmt.Sprintf("Delete folder: proton drive items delete %s", folder),
		"drive", "items", "delete", folder)
	runOK(t, "drive", "items", "upload", "--recursive", tree, folder)

	into := t.TempDir()
	runOK(t, "drive", "items", "download", "--recursive", "--dest-dir", into, folder+"/tree")
	landed := filepath.Join(into, "tree", "a.txt")
	fi, err := os.Stat(landed)
	if err != nil {
		t.Fatal(err)
	}
	if gap := fi.ModTime().Sub(stamped); gap > 2*time.Second || gap < -2*time.Second {
		t.Errorf("the download is stamped %v, want the %v it has in Drive", fi.ModTime(), stamped)
	}

	_, stderr := runOKStderr(t, "drive", "items", "download", "--recursive", "--dest-dir", into, folder+"/tree")
	assertContains(t, stderr, "2 files are already there and unchanged")

	if err := os.WriteFile(landed, []byte("changed here"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, stderr, code := run(t, "drive", "items", "download", "--recursive", "--dest-dir", into, folder+"/tree")
	if code != 1 {
		t.Errorf("exit %d, want 1 for a file that differs", code)
	}
	assertContains(t, stderr, "differ from the ones in Drive")
	runOK(t, "drive", "items", "download", "--recursive", "--force", "--dest-dir", into, folder+"/tree")
	if got, err := os.ReadFile(landed); err != nil || string(got) != "A" {
		t.Errorf("--force left %q, %v", got, err)
	}
}
