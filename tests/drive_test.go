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
	if len(stdout) < 10 {
		t.Error("expected non-empty ls output for root")
	}
	// Root listing should show FILE or DIR entries
	assertContains(t, stdout, "DIR")
}

func TestDriveLsSubfolder(t *testing.T) {
	skipIfNoCredentials(t)
	name := testID() + "-lssub"
	runOK(t, "drive", "mkdir", "/"+name)
	cleanupRun(t, fmt.Sprintf("Delete folder: proton-cli drive rm /%s", name),
		"drive", "rm", "/"+name)

	// Empty folder — should exit 0 with no error
	runOK(t, "drive", "ls", "/"+name)
}

func TestDriveLsJSON(t *testing.T) {
	skipIfNoCredentials(t)
	arr := runJSONArray(t, "drive", "ls")
	if len(arr) == 0 {
		t.Skip("root is empty")
	}
	entry := arr[0].(map[string]interface{})
	if entry["DecryptedName"] == nil || entry["DecryptedName"] == "" {
		t.Error("entry missing DecryptedName")
	}
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
	assertContains(t, stdout, "DIR")
}

func TestDriveUploadAndLs(t *testing.T) {
	skipIfNoCredentials(t)
	folder := testID() + "-upload"
	runOK(t, "drive", "mkdir", "/"+folder)
	cleanupRun(t, fmt.Sprintf("Delete folder: proton-cli drive rm /%s", folder),
		"drive", "rm", "/"+folder)

	tmpFile := filepath.Join(t.TempDir(), "upload-test.txt")
	_ = os.WriteFile(tmpFile, []byte("upload integration test content"), 0644)

	runOK(t, "drive", "upload", tmpFile, "/"+folder)

	stdout := runOK(t, "drive", "ls", "/"+folder)
	assertContains(t, stdout, "upload-test.txt")
	assertContains(t, stdout, "FILE")
}

func TestDriveUploadToRoot(t *testing.T) {
	skipIfNoCredentials(t)
	fileName := testID() + "-rootfile.txt"
	tmpFile := filepath.Join(t.TempDir(), fileName)
	_ = os.WriteFile(tmpFile, []byte("root upload test"), 0644)

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
	_ = os.WriteFile(tmpFile, []byte(content), 0644)
	runOK(t, "drive", "upload", tmpFile, "/"+folder)

	outPath := filepath.Join(t.TempDir(), "dl-output.txt")
	runOK(t, "drive", "download", "/"+folder+"/dl-source.txt", outPath)

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("failed to read downloaded file: %v", err)
	}
	if string(data) != content {
		t.Errorf("downloaded content: got %q, want %q", string(data), content)
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
	_ = os.WriteFile(tmpFile, []byte(content), 0644)
	runOK(t, "drive", "upload", tmpFile, "/"+folder)

	stdout := runOK(t, "drive", "download", "/"+folder+"/stdout-src.txt")
	if stdout != content {
		t.Errorf("stdout download: got %q, want %q", stdout, content)
	}
}

func TestDriveRename(t *testing.T) {
	skipIfNoCredentials(t)
	folder := testID() + "-rename"
	runOK(t, "drive", "mkdir", "/"+folder)
	cleanupRun(t, fmt.Sprintf("Delete folder: proton-cli drive rm /%s", folder),
		"drive", "rm", "/"+folder)

	tmpFile := filepath.Join(t.TempDir(), "before.txt")
	_ = os.WriteFile(tmpFile, []byte("rename test"), 0644)
	runOK(t, "drive", "upload", tmpFile, "/"+folder)

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
	_ = os.WriteFile(tmpFile, []byte("move test"), 0644)
	runOK(t, "drive", "upload", tmpFile, "/"+folderA)

	runOK(t, "drive", "mv", "/"+folderA+"/moveme.txt", "/"+folderB)

	outA := runOK(t, "drive", "ls", "/"+folderA)
	assertNotContains(t, outA, "moveme.txt")

	outB := runOK(t, "drive", "ls", "/"+folderB)
	assertContains(t, outB, "moveme.txt")
}

func TestDriveRm(t *testing.T) {
	skipIfNoCredentials(t)
	folder := testID() + "-rm"
	runOK(t, "drive", "mkdir", "/"+folder)

	tmpFile := filepath.Join(t.TempDir(), "deleteme.txt")
	_ = os.WriteFile(tmpFile, []byte("delete test"), 0644)
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
	_ = os.WriteFile(tmpFile, []byte("permanent delete test"), 0644)
	runOK(t, "drive", "upload", tmpFile, "/"+folder)

	runOK(t, "drive", "rm", "--permanent", "/"+folder+"/permdel.txt")
	stdout := runOK(t, "drive", "ls", "/"+folder)
	assertNotContains(t, stdout, "permdel.txt")

	runOK(t, "drive", "rm", "/"+folder)
}

func TestDriveTrashList(t *testing.T) {
	skipIfNoCredentials(t)
	folder := testID() + "-trashls"
	runOK(t, "drive", "mkdir", "/"+folder)

	runOK(t, "drive", "rm", "/"+folder)

	stdout, _, code := run(t, "drive", "trash", "list")
	if code != 0 {
		t.Fatalf("trash list failed: exit %d", code)
	}
	_ = stdout

	runOK(t, "drive", "trash", "empty")
}

func TestDriveTrashEmpty(t *testing.T) {
	skipIfNoCredentials(t)
	folder := testID() + "-trashempty"
	runOK(t, "drive", "mkdir", "/"+folder)
	runOK(t, "drive", "rm", "/"+folder)

	runOK(t, "drive", "trash", "empty")
}
