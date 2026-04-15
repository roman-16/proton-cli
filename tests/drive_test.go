package tests

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestDriveLsRoot(t *testing.T) {
	skipIfNoCredentials(t)
	stdout := runOK(t, "drive", "ls")
	// Root should have at least some entries
	if len(stdout) < 10 {
		t.Error("expected non-empty ls output for root")
	}
}

func TestDriveLsSubfolder(t *testing.T) {
	skipIfNoCredentials(t)
	// Create a folder to list
	name := testID() + "-lssub"
	runOK(t, "drive", "mkdir", "/"+name)
	cleanupRun(t, fmt.Sprintf("Delete folder: proton-cli drive rm /%s", name),
		"drive", "rm", "/"+name)

	stdout := runOK(t, "drive", "ls", "/"+name)
	_ = stdout // empty folder is fine
}

func TestDriveLsJSON(t *testing.T) {
	skipIfNoCredentials(t)
	arr := runJSONArray(t, "drive", "ls")
	_ = arr // just verify it's valid JSON array
}

func TestDriveMkdirRoot(t *testing.T) {
	skipIfNoCredentials(t)
	name := testID() + "-mkdir"
	runOK(t, "drive", "mkdir", "/"+name)
	cleanupRun(t, fmt.Sprintf("Delete folder: proton-cli drive rm /%s", name),
		"drive", "rm", "/"+name)

	stdout := runOK(t, "drive", "ls")
	assertContains(t, stdout, name)
}

func TestDriveMkdirNested(t *testing.T) {
	skipIfNoCredentials(t)
	parent := testID() + "-parent"
	child := "child"

	runOK(t, "drive", "mkdir", "/"+parent)
	cleanupRun(t, fmt.Sprintf("Delete folder: proton-cli drive rm /%s", parent),
		"drive", "rm", "/"+parent)

	runOK(t, "drive", "mkdir", "/"+parent+"/"+child)

	stdout := runOK(t, "drive", "ls", "/"+parent)
	assertContains(t, stdout, child)
}

func TestDriveUploadAndLs(t *testing.T) {
	skipIfNoCredentials(t)
	folder := testID() + "-upload"
	runOK(t, "drive", "mkdir", "/"+folder)
	cleanupRun(t, fmt.Sprintf("Delete folder: proton-cli drive rm /%s", folder),
		"drive", "rm", "/"+folder)

	// Create temp file
	tmpFile := filepath.Join(t.TempDir(), "upload-test.txt")
	os.WriteFile(tmpFile, []byte("upload integration test content"), 0644)

	// Upload
	runOK(t, "drive", "upload", tmpFile, "/"+folder)

	// Verify
	stdout := runOK(t, "drive", "ls", "/"+folder)
	assertContains(t, stdout, "upload-test.txt")
}

func TestDriveUploadToRoot(t *testing.T) {
	skipIfNoCredentials(t)
	fileName := testID() + "-rootfile.txt"
	tmpFile := filepath.Join(t.TempDir(), fileName)
	os.WriteFile(tmpFile, []byte("root upload test"), 0644)

	runOK(t, "drive", "upload", tmpFile)
	cleanupRun(t, fmt.Sprintf("Delete file: proton-cli drive rm /%s", fileName),
		"drive", "rm", "/"+fileName)

	stdout := runOK(t, "drive", "ls")
	assertContains(t, stdout, fileName)
}

func TestDriveDownloadToFile(t *testing.T) {
	skipIfNoCredentials(t)
	folder := testID() + "-download"
	runOK(t, "drive", "mkdir", "/"+folder)
	cleanupRun(t, fmt.Sprintf("Delete folder: proton-cli drive rm /%s", folder),
		"drive", "rm", "/"+folder)

	content := "download test content 12345"
	tmpFile := filepath.Join(t.TempDir(), "dl-source.txt")
	os.WriteFile(tmpFile, []byte(content), 0644)
	runOK(t, "drive", "upload", tmpFile, "/"+folder)

	// Download
	outPath := filepath.Join(t.TempDir(), "dl-output.txt")
	runOK(t, "drive", "download", "/"+folder+"/dl-source.txt", outPath)

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("failed to read downloaded file: %v", err)
	}
	if string(data) != content {
		t.Errorf("downloaded content mismatch: got %q, want %q", string(data), content)
	}
}

func TestDriveDownloadToStdout(t *testing.T) {
	skipIfNoCredentials(t)
	folder := testID() + "-dlstdout"
	runOK(t, "drive", "mkdir", "/"+folder)
	cleanupRun(t, fmt.Sprintf("Delete folder: proton-cli drive rm /%s", folder),
		"drive", "rm", "/"+folder)

	content := "stdout download test"
	tmpFile := filepath.Join(t.TempDir(), "stdout-src.txt")
	os.WriteFile(tmpFile, []byte(content), 0644)
	runOK(t, "drive", "upload", tmpFile, "/"+folder)

	stdout := runOK(t, "drive", "download", "/"+folder+"/stdout-src.txt")
	if stdout != content {
		t.Errorf("stdout download mismatch: got %q, want %q", stdout, content)
	}
}

func TestDriveRename(t *testing.T) {
	skipIfNoCredentials(t)
	folder := testID() + "-rename"
	runOK(t, "drive", "mkdir", "/"+folder)
	cleanupRun(t, fmt.Sprintf("Delete folder: proton-cli drive rm /%s", folder),
		"drive", "rm", "/"+folder)

	tmpFile := filepath.Join(t.TempDir(), "before.txt")
	os.WriteFile(tmpFile, []byte("rename test"), 0644)
	runOK(t, "drive", "upload", tmpFile, "/"+folder)

	// Rename
	runOK(t, "drive", "rename", "/"+folder+"/before.txt", "after.txt")

	stdout := runOK(t, "drive", "ls", "/"+folder)
	assertContains(t, stdout, "after.txt")
	assertNotContains(t, stdout, "before.txt")
}

func TestDriveRenameFolder(t *testing.T) {
	skipIfNoCredentials(t)
	original := testID() + "-origfolder"
	renamed := testID() + "-renamed"

	runOK(t, "drive", "mkdir", "/"+original)
	// Cleanup uses renamed name since that's what it'll be after the test
	cleanupRun(t, fmt.Sprintf("Delete folder: proton-cli drive rm /%s", renamed),
		"drive", "rm", "/"+renamed)

	runOK(t, "drive", "rename", "/"+original, renamed)

	stdout := runOK(t, "drive", "ls")
	assertContains(t, stdout, renamed)
	assertNotContains(t, stdout, original)
}

func TestDriveMv(t *testing.T) {
	skipIfNoCredentials(t)
	folderA := testID() + "-mvA"
	folderB := testID() + "-mvB"

	runOK(t, "drive", "mkdir", "/"+folderA)
	cleanupRun(t, fmt.Sprintf("Delete folder: proton-cli drive rm /%s", folderA),
		"drive", "rm", "/"+folderA)

	runOK(t, "drive", "mkdir", "/"+folderB)
	cleanupRun(t, fmt.Sprintf("Delete folder: proton-cli drive rm /%s", folderB),
		"drive", "rm", "/"+folderB)

	tmpFile := filepath.Join(t.TempDir(), "moveme.txt")
	os.WriteFile(tmpFile, []byte("move test"), 0644)
	runOK(t, "drive", "upload", tmpFile, "/"+folderA)

	// Move from A to B
	runOK(t, "drive", "mv", "/"+folderA+"/moveme.txt", "/"+folderB)

	// Verify
	outA := runOK(t, "drive", "ls", "/"+folderA)
	assertNotContains(t, outA, "moveme.txt")

	outB := runOK(t, "drive", "ls", "/"+folderB)
	assertContains(t, outB, "moveme.txt")
}

func TestDriveRm(t *testing.T) {
	skipIfNoCredentials(t)
	folder := testID() + "-rm"
	runOK(t, "drive", "mkdir", "/"+folder)
	// No cleanup needed — we're testing rm itself

	tmpFile := filepath.Join(t.TempDir(), "deleteme.txt")
	os.WriteFile(tmpFile, []byte("delete test"), 0644)
	runOK(t, "drive", "upload", tmpFile, "/"+folder)

	// Delete file
	runOK(t, "drive", "rm", "/"+folder+"/deleteme.txt")
	stdout := runOK(t, "drive", "ls", "/"+folder)
	assertNotContains(t, stdout, "deleteme.txt")

	// Delete folder
	runOK(t, "drive", "rm", "/"+folder)
	rootOut := runOK(t, "drive", "ls")
	assertNotContains(t, rootOut, folder)
}

func TestDriveRmPermanent(t *testing.T) {
	skipIfNoCredentials(t)
	folder := testID() + "-rmperm"
	runOK(t, "drive", "mkdir", "/"+folder)

	tmpFile := filepath.Join(t.TempDir(), "permdel.txt")
	os.WriteFile(tmpFile, []byte("permanent delete test"), 0644)
	runOK(t, "drive", "upload", tmpFile, "/"+folder)

	// Permanent delete
	runOK(t, "drive", "rm", "--permanent", "/"+folder+"/permdel.txt")
	stdout := runOK(t, "drive", "ls", "/"+folder)
	assertNotContains(t, stdout, "permdel.txt")

	// Cleanup folder
	runOK(t, "drive", "rm", "/"+folder)
}

func TestDriveTrashList(t *testing.T) {
	skipIfNoCredentials(t)
	folder := testID() + "-trashls"
	runOK(t, "drive", "mkdir", "/"+folder)

	// Trash it
	runOK(t, "drive", "rm", "/"+folder)

	// List trash
	stdout, _, code := run(t, "drive", "trash", "list")
	if code != 0 {
		t.Fatalf("trash list failed: exit %d", code)
	}
	_ = stdout // just verify it runs

	// Empty trash to clean up
	runOK(t, "drive", "trash", "empty")
}

func TestDriveTrashEmpty(t *testing.T) {
	skipIfNoCredentials(t)
	folder := testID() + "-trashempty"
	runOK(t, "drive", "mkdir", "/"+folder)
	runOK(t, "drive", "rm", "/"+folder)

	// Empty — just verify it exits 0
	runOK(t, "drive", "trash", "empty")
}
