package drive

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"

	"github.com/roman-16/proton-cli/internal/proton"
)

// A computer is the tree the Proton Drive desktop app syncs.
//
// It is a share like any other, so everything that works on a path works inside
// one: the difference is only which root the path starts from. Nothing here
// creates one - a computer arrives by signing the desktop app in on it.

// The operating systems Proton numbers a computer by.
const (
	systemWindows = 1
	systemMacOS   = 2
	systemLinux   = 3
)

// Computer is one computer syncing files to Drive.
type Computer struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	System   string `json:"system"`
	LastSync int64  `json:"last_sync_time,omitempty"`
	Created  int64  `json:"create_time,omitempty"`
	Modified int64  `json:"modify_time,omitempty"`
	ShareID  string `json:"share_id"`
	LinkID   string `json:"link_id"`
	VolumeID string `json:"volume_id"`

	// namedInTheClear records that Proton is holding this computer's name beside
	// the share rather than on its root, which is where a computer named before
	// Proton moved it still keeps it. Renaming has to take that copy away.
	namedInTheClear bool
}

// device is one entry of Proton's own listing, before its name is read.
type device struct {
	Device struct {
		DeviceID     string
		VolumeID     string
		Type         int
		LastSyncTime int64
		CreateTime   int64
		ModifyTime   int64
	}
	Share struct {
		ShareID string
		LinkID  string
		// Name is where the desktop app used to put the computer's name, in the
		// clear. A computer named since carries it on its root instead.
		Name string
	}
}

func systemName(deviceType int) string {
	switch deviceType {
	case systemWindows:
		return "windows"
	case systemMacOS:
		return "macos"
	case systemLinux:
		return "linux"
	}
	return "unknown"
}

// Computers lists what is syncing to Drive.
//
// A computer's name lives on the root of its share, so reading it means opening
// the share - which is also what makes the tree addressable, and is skipped for
// a computer still carrying the name in the clear.
func (s *Service) Computers(ctx context.Context) ([]Computer, error) {
	var r struct{ Devices []device }
	if err := s.C.Decode(ctx, proton.Request{Method: "GET", Path: "/drive/devices"}, &r); err != nil {
		return nil, err
	}
	out := make([]Computer, 0, len(r.Devices))
	for _, d := range r.Devices {
		computer := Computer{
			ID: d.Device.DeviceID, Name: d.Share.Name,
			System:   systemName(d.Device.Type),
			LastSync: d.Device.LastSyncTime,
			Created:  d.Device.CreateTime, Modified: d.Device.ModifyTime,
			ShareID: d.Share.ShareID, LinkID: d.Share.LinkID, VolumeID: d.Device.VolumeID,
			namedInTheClear: d.Share.Name != "",
		}
		if !computer.namedInTheClear {
			dc, err := s.unlockShare(ctx, d.Share.ShareID, d.Share.LinkID, d.Device.VolumeID)
			if err != nil {
				// A computer whose share will not open is reported by its
				// identity rather than dropped: recorded and not counted,
				// because the row is there to act on and the missing name is on
				// the screen.
				slog.DebugContext(ctx, "drive: could not open a computer's share",
					"share", d.Share.ShareID, "error", err)
			} else {
				computer.Name = dc.RootName
			}
		}
		out = append(out, computer)
	}
	return out, nil
}

// OpenComputer opens the tree a computer syncs, so a path can start from it.
func (s *Service) OpenComputer(ctx context.Context, c Computer) (*Context, error) {
	return s.unlockShare(ctx, c.ShareID, c.LinkID, c.VolumeID)
}

// RenameComputer changes what a computer is called.
//
// The name sits on the root of its share, where there is no folder for it to be
// unique in - so Proton is handed a hash nothing will ever look the name up by.
// A computer named before Proton moved the name onto the root carries it in the
// clear as well, and that copy is dropped, or the old name is what every client
// would go on showing.
func (s *Service) RenameComputer(ctx context.Context, c Computer, newName string) error {
	dc, err := s.OpenComputer(ctx, c)
	if err != nil {
		return err
	}
	root, err := s.ResolvePath(ctx, dc, "/")
	if err != nil {
		return err
	}
	hash, err := unhashedName()
	if err != nil {
		return err
	}
	if err := s.rename(ctx, dc, root, newName, hash, root.Link.Hash); err != nil {
		return err
	}
	if !c.namedInTheClear {
		return nil
	}
	return s.C.Decode(ctx, proton.Request{
		Method: "PUT", Path: "/drive/devices/" + c.ID,
		Body: map[string]any{"Share": map[string]any{"Name": ""}},
	}, nil)
}

// DeleteComputer takes a computer off the account.
func (s *Service) DeleteComputer(ctx context.Context, c Computer) error {
	return s.C.Decode(ctx, proton.Request{Method: "DELETE", Path: "/drive/devices/" + c.ID}, nil)
}

// unhashedName is what stands in for a lookup hash where there is nothing to
// look the name up under. Proton stores it and no sibling is ever compared
// against it, so it is random rather than derived from anything.
func unhashedName() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}
