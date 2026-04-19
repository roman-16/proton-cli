package tests

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// ── list ──

func TestDriveItemsList(t *testing.T) {
	skipIfNoCredentials(t)
	stdout := runOK(t, "drive", "items", "list")
	assertContains(t, stdout, "NAME")
}

func TestDriveItemsListJSONFieldNames(t *testing.T) {
	skipIfNoCredentials(t)
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

// ── upload / download lifecycle ──

func TestDriveItemsUploadDownload(t *testing.T) {
	skipIfNoCredentials(t)
	folder := "/" + testID() + "-upload"
	tmp := t.TempDir()
	src := filepath.Join(tmp, "payload.txt")
	want := "hello from drive test"
	_ = os.WriteFile(src, []byte(want), 0644)

	runOK(t, "drive", "folders", "create", folder)
	cleanupRun(t, fmt.Sprintf("Delete folder: proton-cli drive items delete --permanent %s", folder),
		"drive", "items", "delete", "--permanent", folder)

	runOK(t, "drive", "items", "upload", src, folder)
	out := filepath.Join(tmp, "out.txt")
	runOK(t, "drive", "items", "download", folder+"/payload.txt", out)
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("downloaded file not readable: %v", err)
	}
	if string(data) != want {
		t.Errorf("content mismatch: got %q, want %q", string(data), want)
	}
}

func TestDriveItemsUploadFromStdin(t *testing.T) {
	skipIfNoCredentials(t)
	folder := "/" + testID() + "-stdin"
	runOK(t, "drive", "folders", "create", folder)
	cleanupRun(t, fmt.Sprintf("Delete folder: proton-cli drive items delete --permanent %s", folder),
		"drive", "items", "delete", "--permanent", folder)

	payload := []byte("piped payload\n")
	cmd := exec.Command(binaryPath, "drive", "items", "upload", "-", folder)
	cmd.Stdin = bytes.NewReader(payload)
	cmd.Env = os.Environ()
	b, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("stdin upload failed: %v\noutput: %s", err, string(b))
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
	stdout := runOK(t, "drive", "items", "download", folder+"/"+name, "-")
	if !strings.Contains(stdout, "piped payload") {
		t.Errorf("stdout download mismatch: %q", stdout)
	}
}

func TestDriveItemsDownloadToStdoutNoArg(t *testing.T) {
	skipIfNoCredentials(t)
	folder := "/" + testID() + "-stdout"
	tmp := t.TempDir()
	src := filepath.Join(tmp, "p.txt")
	_ = os.WriteFile(src, []byte("stdoutpayload"), 0644)

	runOK(t, "drive", "folders", "create", folder)
	cleanupRun(t, fmt.Sprintf("Delete folder: proton-cli drive items delete --permanent %s", folder),
		"drive", "items", "delete", "--permanent", folder)
	runOK(t, "drive", "items", "upload", src, folder)

	// No DEST arg → stdout
	stdout := runOK(t, "drive", "items", "download", folder+"/p.txt")
	assertContains(t, stdout, "stdoutpayload")
}

func TestDriveItemsUploadRecursive(t *testing.T) {
	skipIfNoCredentials(t)
	folder := "/" + testID() + "-rec"
	tmp := t.TempDir()
	tree := filepath.Join(tmp, "tree")
	_ = os.MkdirAll(filepath.Join(tree, "sub1"), 0755)
	_ = os.MkdirAll(filepath.Join(tree, "sub2", "deep"), 0755)
	_ = os.WriteFile(filepath.Join(tree, "a.txt"), []byte("A"), 0644)
	_ = os.WriteFile(filepath.Join(tree, "sub1", "b.txt"), []byte("B"), 0644)
	_ = os.WriteFile(filepath.Join(tree, "sub2", "deep", "d.txt"), []byte("D"), 0644)

	runOK(t, "drive", "folders", "create", folder)
	cleanupRun(t, fmt.Sprintf("Delete folder: proton-cli drive items delete --permanent %s", folder),
		"drive", "items", "delete", "--permanent", folder)

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
	skipIfNoCredentials(t)
	folder := "/" + testID() + "-big"
	tmp := t.TempDir()
	src := filepath.Join(tmp, "big.bin")
	big := make([]byte, 8*1024*1024) // 8 MB → two 4 MB blocks
	if _, err := io.ReadFull(rand.Reader, big); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(src, big, 0644)
	hWant := sha256.Sum256(big)

	runOK(t, "drive", "folders", "create", folder)
	cleanupRun(t, fmt.Sprintf("Delete folder: proton-cli drive items delete --permanent %s", folder),
		"drive", "items", "delete", "--permanent", folder)

	runOK(t, "drive", "items", "upload", src, folder)
	out := filepath.Join(tmp, "out.bin")
	runOK(t, "drive", "items", "download", folder+"/big.bin", out)

	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	hGot := sha256.Sum256(got)
	if hex.EncodeToString(hGot[:]) != hex.EncodeToString(hWant[:]) {
		t.Errorf("sha256 mismatch after multi-block round-trip")
	}
}

// ── rename / move (re-encryption) ──

func TestDriveItemsRename(t *testing.T) {
	skipIfNoCredentials(t)
	folder := "/" + testID() + "-rn"
	tmp := t.TempDir()
	_ = os.WriteFile(filepath.Join(tmp, "orig.txt"), []byte("renameme"), 0644)
	runOK(t, "drive", "folders", "create", folder)
	cleanupRun(t, fmt.Sprintf("Delete folder: proton-cli drive items delete --permanent %s", folder),
		"drive", "items", "delete", "--permanent", folder)
	runOK(t, "drive", "items", "upload", filepath.Join(tmp, "orig.txt"), folder)

	runOK(t, "drive", "items", "rename", folder+"/orig.txt", "new.txt")

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
	runOK(t, "drive", "items", "download", folder+"/new.txt", out)
	if b, _ := os.ReadFile(out); string(b) != "renameme" {
		t.Errorf("content mismatch after rename: %q", string(b))
	}
}

func TestDriveItemsMove(t *testing.T) {
	skipIfNoCredentials(t)
	src := "/" + testID() + "-src"
	dst := "/" + testID() + "-dst"
	tmp := t.TempDir()
	_ = os.WriteFile(filepath.Join(tmp, "f.txt"), []byte("moveme"), 0644)

	runOK(t, "drive", "folders", "create", src)
	runOK(t, "drive", "folders", "create", dst)
	cleanupRun(t, fmt.Sprintf("Delete src: proton-cli drive items delete --permanent %s", src),
		"drive", "items", "delete", "--permanent", src)
	cleanupRun(t, fmt.Sprintf("Delete dst: proton-cli drive items delete --permanent %s", dst),
		"drive", "items", "delete", "--permanent", dst)
	runOK(t, "drive", "items", "upload", filepath.Join(tmp, "f.txt"), src)

	runOK(t, "drive", "items", "move", src+"/f.txt", dst)

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
	runOK(t, "drive", "items", "download", dst+"/f.txt", out)
	if b, _ := os.ReadFile(out); string(b) != "moveme" {
		t.Errorf("content mismatch after move: %q", string(b))
	}
}

// ── delete + trash ──

func TestDriveItemsDeleteAndTrashRestore(t *testing.T) {
	skipIfNoCredentials(t)
	folder := "/" + testID() + "-trash"
	runOK(t, "drive", "folders", "create", folder)
	cleanupRun(t, fmt.Sprintf("Final delete: proton-cli drive items delete --permanent %s", folder),
		"drive", "items", "delete", "--permanent", folder)

	// Non-permanent → trash
	runOK(t, "drive", "items", "delete", folder)

	// Should appear in trash
	entries := runJSONArray(t, "drive", "trash", "list")
	var linkID string
	for _, e := range entries {
		m := e.(map[string]interface{})
		typeCode, _ := m["type"].(float64)
		if int(typeCode) == 1 {
			linkID, _ = m["link_id"].(string)
			break
		}
	}
	if linkID == "" {
		t.Fatal("expected at least one folder in trash after delete")
	}

	// Restore (IDs only — trashed names are encrypted)
	runOK(t, "drive", "trash", "restore", "--", linkID)

	// It should be back in root
	top := runJSONArray(t, "drive", "items", "list")
	found := false
	folderName := strings.TrimPrefix(folder, "/")
	for _, c := range top {
		if c.(map[string]interface{})["name"].(string) == folderName {
			found = true
		}
	}
	if !found {
		t.Error("restored folder should be back in root")
	}
}

// ── batch filters (all dry-run) ──

func TestDriveBatchDeletePatternDryRun(t *testing.T) {
	skipIfNoCredentials(t)
	folder := "/" + testID() + "-pat"
	tmp := t.TempDir()
	for _, n := range []string{"a.log", "b.log", "keep.txt"} {
		_ = os.WriteFile(filepath.Join(tmp, n), []byte("x"), 0644)
	}
	runOK(t, "drive", "folders", "create", folder)
	cleanupRun(t, fmt.Sprintf("Delete folder: proton-cli drive items delete --permanent %s", folder),
		"drive", "items", "delete", "--permanent", folder)
	for _, n := range []string{"a.log", "b.log", "keep.txt"} {
		runOK(t, "drive", "items", "upload", filepath.Join(tmp, n), folder)
	}

	_, stderr := runOKStderr(t, "--dry-run", "drive", "items", "delete",
		"--pattern", "*.log", "--scope", folder, "--recursive")
	assertContains(t, stderr, "would delete 2 item(s)")
	assertContains(t, stderr, "a.log")
	assertContains(t, stderr, "b.log")
	assertNotContains(t, stderr, "keep.txt")
}

func TestDriveBatchDeleteRequiresInput(t *testing.T) {
	skipIfNoCredentials(t)
	_, stderr, code := run(t, "drive", "items", "delete")
	if code == 0 {
		t.Error("expected error when no PATH and no filter given")
	}
	assertContains(t, stderr, "no paths selected")
}

func TestDriveBatchDeleteAllRequiresScope(t *testing.T) {
	skipIfNoCredentials(t)
	_, stderr, code := run(t, "drive", "items", "delete", "--all")
	if code == 0 {
		t.Error("expected --all alone to be rejected")
	}
	assertContains(t, stderr, "--all requires")
}

// ── folders ──

func TestDriveFoldersCreate(t *testing.T) {
	skipIfNoCredentials(t)
	folder := "/" + testID() + "-folder"
	runOK(t, "drive", "folders", "create", folder)
	cleanupRun(t, fmt.Sprintf("Delete folder: proton-cli drive items delete --permanent %s", folder),
		"drive", "items", "delete", "--permanent", folder)

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
