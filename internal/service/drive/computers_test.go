package drive

import (
	"context"
	"encoding/hex"
	"testing"
)

const testDeviceID = "device-1"

// devices is the listing Proton answers with, for one computer.
func devices(t *testing.T, deviceType int, legacyName string, lastSync int64) string {
	t.Helper()
	return object(t, map[string]any{"Devices": []any{map[string]any{
		"Device": map[string]any{
			"DeviceID": testDeviceID, "VolumeID": testVolumeID, "Type": deviceType,
			"LastSyncTime": lastSync, "CreateTime": 1757000000, "ModifyTime": 1757100000,
		},
		"Share": map[string]any{
			"ShareID": testShareID, "LinkID": testRootID, "Name": legacyName,
		},
	}}})
}

func onlyComputer(t *testing.T, s *Service) Computer {
	t.Helper()
	computers, err := s.Computers(context.Background())
	if err != nil {
		t.Fatalf("Computers: %v", err)
	}
	if len(computers) != 1 {
		t.Fatalf("got %d computers, want 1", len(computers))
	}
	return computers[0]
}

// A computer's name lives on the root of its share, and reading it is what
// opening the share is for.
func TestComputersReadTheNameOffTheRoot(t *testing.T) {
	tr := newTree(t, shareTypeDevice, protonFolder, "Work laptop")
	s, _ := tr.service(map[string]string{"GET /drive/devices": devices(t, systemWindows, "", 1757200000)})

	computer := onlyComputer(t, s)
	if computer.Name != "Work laptop" {
		t.Errorf("Name = %q, want the name on the root", computer.Name)
	}
	if computer.System != "windows" {
		t.Errorf("System = %q, want windows", computer.System)
	}
	if computer.LastSync != 1757200000 {
		t.Errorf("LastSync = %d, want the time Proton reported", computer.LastSync)
	}
	if computer.ID != testDeviceID || computer.ShareID != testShareID ||
		computer.LinkID != testRootID || computer.VolumeID != testVolumeID {
		t.Errorf("the computer is not addressable: %+v", computer)
	}
}

// A computer named before Proton moved the name onto the root carries it in the
// clear, and then there is nothing to open a share for.
func TestComputersPreferTheNameProtonHoldsInTheClear(t *testing.T) {
	tr := newTree(t, shareTypeDevice, protonFolder, "Work laptop")
	s, doer := tr.service(map[string]string{"GET /drive/devices": devices(t, systemMacOS, "Old PC", 0)})

	computer := onlyComputer(t, s)
	if computer.Name != "Old PC" {
		t.Errorf("Name = %q, want the name Proton holds", computer.Name)
	}
	if doer.sent("GET", "/drive/shares/"+testShareID) {
		t.Error("the share was opened for a name that was already there")
	}
}

// A computer whose share will not open is still listed: knowing it is there is
// what lets somebody rename or remove it.
func TestComputersListOneWhoseShareWillNotOpen(t *testing.T) {
	tr := newTree(t, shareTypeDevice, protonFolder, "Work laptop")
	s, _ := tr.service(map[string]string{
		"GET /drive/devices":               devices(t, systemLinux, "", 0),
		"GET /drive/shares/" + testShareID: `{"AddressID":"nobody","Key":"not a key"}`,
	})

	computer := onlyComputer(t, s)
	if computer.Name != "" {
		t.Errorf("Name = %q, want it left empty", computer.Name)
	}
	if computer.ID != testDeviceID {
		t.Errorf("ID = %q, want the computer still addressable", computer.ID)
	}
	if computer.System != "linux" {
		t.Errorf("System = %q, want linux", computer.System)
	}
}

func TestSystemName(t *testing.T) {
	for number, want := range map[int]string{
		systemWindows: "windows", systemMacOS: "macos", systemLinux: "linux", 9: "unknown",
	} {
		if got := systemName(number); got != want {
			t.Errorf("systemName(%d) = %q, want %q", number, got, want)
		}
	}
}

// Renaming a computer renames the root of its share: a name sealed to the share
// key, a hash nothing will look it up under, and the hash it releases.
func TestRenameComputerWritesTheNameOntoTheRoot(t *testing.T) {
	tr := newTree(t, shareTypeDevice, protonFolder, "Work laptop")
	s, doer := tr.service(map[string]string{"GET /drive/devices": devices(t, systemWindows, "", 0)})

	computer := onlyComputer(t, s)
	if err := s.RenameComputer(context.Background(), computer, "Office PC"); err != nil {
		t.Fatalf("RenameComputer: %v", err)
	}

	req := doer.last()
	if req.Method != "PUT" || req.Path != "/drive/shares/"+testShareID+"/links/"+testRootID+"/rename" {
		t.Fatalf("renamed with %s %s", req.Method, req.Path)
	}
	body, ok := req.Body.(map[string]any)
	if !ok {
		t.Fatalf("body is not map[string]any: %T", req.Body)
	}
	name, err := decryptName(body["Name"].(string), tr.shareKR)
	if err != nil {
		t.Fatalf("the new name is not sealed to the share key: %v", err)
	}
	if name != "Office PC" {
		t.Errorf("the new name is %q", name)
	}
	if body["OriginalHash"] != testRootHash {
		t.Errorf("OriginalHash = %v, want the hash the root carries", body["OriginalHash"])
	}
	hash, _ := body["Hash"].(string)
	if _, err := hex.DecodeString(hash); err != nil || len(hash) != 64 || hash == testRootHash {
		t.Errorf("Hash = %q, want a fresh one nothing looks the name up under", hash)
	}
	if body["NameSignatureEmail"] != testAddrMail {
		t.Errorf("NameSignatureEmail = %v, want the address that signed it", body["NameSignatureEmail"])
	}
	if doer.sent("PUT", "/drive/devices/"+testDeviceID) {
		t.Error("a computer with no name in the clear had one cleared")
	}
}

// A name Proton still holds in the clear is dropped as the root takes the new
// one, or every client would go on showing the old one.
func TestRenameComputerDropsTheNameHeldInTheClear(t *testing.T) {
	tr := newTree(t, shareTypeDevice, protonFolder, "Work laptop")
	s, doer := tr.service(map[string]string{"GET /drive/devices": devices(t, systemWindows, "Old PC", 0)})

	computer := onlyComputer(t, s)
	if err := s.RenameComputer(context.Background(), computer, "Office PC"); err != nil {
		t.Fatalf("RenameComputer: %v", err)
	}

	req := doer.last()
	if req.Method != "PUT" || req.Path != "/drive/devices/"+testDeviceID {
		t.Fatalf("cleared the old name with %s %s", req.Method, req.Path)
	}
	body, ok := req.Body.(map[string]any)
	if !ok {
		t.Fatalf("body is not map[string]any: %T", req.Body)
	}
	share, ok := body["Share"].(map[string]any)
	if !ok || share["Name"] != "" {
		t.Errorf("body = %v, want the share's name emptied", body)
	}
}

func TestDeleteComputerRemovesTheDevice(t *testing.T) {
	tr := newTree(t, shareTypeDevice, protonFolder, "Work laptop")
	s, doer := tr.service(map[string]string{"GET /drive/devices": devices(t, systemWindows, "", 0)})

	if err := s.DeleteComputer(context.Background(), onlyComputer(t, s)); err != nil {
		t.Fatalf("DeleteComputer: %v", err)
	}
	req := doer.last()
	if req.Method != "DELETE" || req.Path != "/drive/devices/"+testDeviceID {
		t.Errorf("deleted with %s %s", req.Method, req.Path)
	}
	if req.Body != nil {
		t.Errorf("body = %v, want none", req.Body)
	}
}

// Opening a computer gives a context rooted at what it syncs, which is what puts
// every path command inside it.
func TestOpenComputerRootsTheTreeAtTheComputer(t *testing.T) {
	tr := newTree(t, shareTypeDevice, protonFolder, "Work laptop")
	s, _ := tr.service(map[string]string{"GET /drive/devices": devices(t, systemWindows, "", 0)})

	dc, err := s.OpenComputer(context.Background(), onlyComputer(t, s))
	if err != nil {
		t.Fatalf("OpenComputer: %v", err)
	}
	if dc.ShareID != testShareID || dc.RootLinkID != testRootID || dc.VolumeID != testVolumeID {
		t.Errorf("the context is not the computer's: %+v", dc)
	}
	if dc.RootName != "Work laptop" {
		t.Errorf("RootName = %q, want the computer's name", dc.RootName)
	}
	if _, err := dc.RootKR(); err != nil {
		t.Errorf("the computer's root will not open: %v", err)
	}
}

// Every name a rename generates is its own, so two computers cannot end up
// hashed alike.
func TestUnhashedNameIsNeverTheSameTwice(t *testing.T) {
	first, err := unhashedName()
	if err != nil {
		t.Fatalf("unhashedName: %v", err)
	}
	second, err := unhashedName()
	if err != nil {
		t.Fatalf("unhashedName: %v", err)
	}
	if first == second {
		t.Error("two hashes are identical")
	}
	if _, err := hex.DecodeString(first); err != nil {
		t.Errorf("the hash is not hex: %v", err)
	}
}
