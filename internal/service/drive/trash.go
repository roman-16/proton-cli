package drive

import (
	"context"
	"fmt"
	"log/slog"

	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"

	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/proton"
)

// The trash is a property of a volume, and an account has more than one: files
// live on the default volume and photos on their own. Proton's own client reads
// both and shows them as one list (useTrashView), so this does too - anything
// less would be a listing that misses what emptying the trash destroys.
const trashPageSize = 150

// TrashRef names one trashed item. A link ID means nothing without the volume
// that holds it and the share whose keys read it.
type TrashRef struct {
	VolumeID string
	ShareID  string
	LinkID   string
}

type TrashEntry struct {
	ShareID string `json:"share_id"`
	LinkID  string `json:"link_id"`
	// Name is empty when the keys that would read it are not this account's,
	// which is worth listing anyway: knowing the item is there is what lets
	// somebody act on it.
	Name    string `json:"name"`
	Type    string `json:"type"`
	Size    int64  `json:"size"`
	Trashed int64  `json:"trashed"`
}

// TrashRefs is everything in the trash, on every volume the account has.
//
// It is the cheap half of a listing: identity only, no metadata and no keys, so
// counting the trash costs a request per volume per page and nothing else.
func (s *Service) TrashRefs(ctx context.Context, dc *Context) ([]TrashRef, error) {
	volumes, err := s.trashVolumes(ctx, dc)
	if err != nil {
		return nil, err
	}
	var refs []TrashRef
	for _, volumeID := range volumes {
		for page := 0; ; page++ {
			var r struct {
				Trash []struct {
					ShareID string
					LinkIDs []string
				}
			}
			if err := s.C.Decode(ctx, proton.Request{
				Method: "GET", Path: "/drive/volumes/" + volumeID + "/trash",
				Query: proton.Query("Page", fmt.Sprintf("%d", page), "PageSize", fmt.Sprintf("%d", trashPageSize)),
			}, &r); err != nil {
				return nil, err
			}
			found := 0
			for _, group := range r.Trash {
				for _, id := range group.LinkIDs {
					refs = append(refs, TrashRef{VolumeID: volumeID, ShareID: group.ShareID, LinkID: id})
					found++
				}
			}
			// A page short of full is the last one. Proton says nothing else about
			// how much is there, and stopping early would be a listing that
			// silently misses what `empty` would destroy.
			if found < trashPageSize {
				break
			}
		}
	}
	return refs, nil
}

// trashVolumes is every volume whose trash is this account's: the one the file
// tree is on, and the photo volume when Proton keeps photos on their own.
func (s *Service) trashVolumes(ctx context.Context, dc *Context) ([]string, error) {
	shares, _, err := s.listShares(ctx)
	if err != nil {
		return nil, err
	}
	volumes := []string{dc.VolumeID}
	for _, sh := range shares {
		if sh.Type == shareTypePhotos && !sh.Locked && sh.VolumeID != "" && sh.VolumeID != dc.VolumeID {
			volumes = append(volumes, sh.VolumeID)
			break
		}
	}
	return volumes, nil
}

// TrashDescribe reads what the given refs are: type, size, when they were
// trashed, and the name, which has to be decrypted with the keys of the share
// each one came from.
//
// Metadata is asked for in batches, the way the web clients ask for it, so a
// large trash costs a request per fifty items rather than one per item.
func (s *Service) TrashDescribe(ctx context.Context, dc *Context, refs []TrashRef) ([]TrashEntry, error) {
	byShare := map[string][]string{}
	order := []string{}
	for _, ref := range refs {
		if _, seen := byShare[ref.ShareID]; !seen {
			order = append(order, ref.ShareID)
		}
		byShare[ref.ShareID] = append(byShare[ref.ShareID], ref.LinkID)
	}

	entries := make([]TrashEntry, 0, len(refs))
	for _, shareID := range order {
		keys := s.namesIn(ctx, dc, shareID)
		for _, batch := range chunk(byShare[shareID], linkBatch) {
			links, err := s.linkMetadata(ctx, shareID, batch)
			if err != nil {
				return nil, err
			}
			for _, link := range links {
				entries = append(entries, TrashEntry{
					ShareID: shareID, LinkID: link.LinkID, Type: linkType(link.Type),
					Size: link.Size, Trashed: link.Trashed,
					Name: keys.name(ctx, &link),
				})
			}
		}
	}
	return entries, nil
}

// linkMetadata asks about several links at once.
func (s *Service) linkMetadata(ctx context.Context, shareID string, linkIDs []string) ([]Link, error) {
	var r struct{ Links []Link }
	if err := s.C.Decode(ctx, proton.Request{
		Method: "POST", Path: "/drive/shares/" + shareID + "/links/fetch_metadata",
		Body: map[string]any{"LinkIDs": linkIDs, "Thumbnails": 0},
	}, &r); err != nil {
		return nil, err
	}
	return r.Links, nil
}

// namesIn reads the names of one share's links.
//
// A name is encrypted to the key of the folder the item is in, so reading one
// means walking that folder's ancestry back to the share it belongs to. The
// walk is remembered, because a trash is usually a handful of folders' worth of
// items and the same ancestors answer for all of them.
//
// A share whose keys will not open leaves every name blank, which is how a
// listing says "there, but not readable".
func (s *Service) namesIn(ctx context.Context, dc *Context, shareID string) *nameReader {
	r := &nameReader{service: s, shareID: shareID, addrKR: dc.AddrKR, rings: map[string]*pgp.KeyRing{}}
	if shareID != dc.ShareID {
		// Anything not on the file tree's own share is on another of the account's -
		// the photo volume's, in practice - and its keys are opened the same way.
		other, err := s.shareByID(ctx, shareID)
		if err != nil {
			slog.Debug("drive: could not open a share holding trashed items", "share", shareID, "error", err)
			return r
		}
		dc = other
	}
	// A trashed item's folder is usually the share's root, whose key ring is the
	// one the walk below bottoms out at.
	rootKR, err := dc.RootKR()
	if err != nil {
		slog.Debug("drive: could not open the root of a share holding trashed items",
			"share", shareID, "error", err)
		return r
	}
	r.rootLinkID, r.rootKR = dc.RootLinkID, rootKR
	return r
}

// shareByID opens one of the account's own shares, named by ID rather than by
// the volume it roots.
func (s *Service) shareByID(ctx context.Context, shareID string) (*Context, error) {
	shares, _, err := s.listShares(ctx)
	if err != nil {
		return nil, err
	}
	for _, sh := range shares {
		if sh.ShareID == shareID {
			return s.unlockShare(ctx, sh.ShareID, sh.LinkID, sh.VolumeID)
		}
	}
	return nil, &errs.NotFound{Kind: "share", Ref: shareID}
}

// nameReader decrypts the names of links within one share.
type nameReader struct {
	service    *Service
	shareID    string
	rootLinkID string
	rootKR     *pgp.KeyRing
	addrKR     *pgp.KeyRing
	rings      map[string]*pgp.KeyRing
}

func (r *nameReader) name(ctx context.Context, link *Link) string {
	if r.rootKR == nil {
		return ""
	}
	if link.Name == "" {
		slog.Debug("drive: a trashed item arrived with no name to read", "link", link.LinkID)
		return ""
	}
	parentKR, err := r.ringOf(ctx, link.ParentLinkID)
	if err != nil {
		slog.Debug("drive: could not open the folder a trashed item was in",
			"link", link.LinkID, "parent", link.ParentLinkID, "error", err)
		return ""
	}
	name, err := decryptName(link.Name, parentKR)
	if err != nil {
		slog.Debug("drive: could not read a trashed item's name", "link", link.LinkID, "error", err)
		return ""
	}
	return name
}

// ringOf is the key ring of one folder, found by walking to the share's root and
// unwrapping back down. Proton's own client does the same thing recursively in
// getLinkPrivateKey.
func (r *nameReader) ringOf(ctx context.Context, linkID string) (*pgp.KeyRing, error) {
	if linkID == "" || linkID == r.rootLinkID {
		return r.rootKR, nil
	}
	if kr, ok := r.rings[linkID]; ok {
		return kr, nil
	}
	link, err := r.service.getLink(ctx, r.shareID, linkID)
	if err != nil {
		return nil, err
	}
	parentKR, err := r.ringOf(ctx, link.ParentLinkID)
	if err != nil {
		return nil, err
	}
	kr, err := unlockNode(link, parentKR, r.addrKR)
	if err != nil {
		return nil, err
	}
	r.rings[linkID] = kr
	return kr, nil
}

// TrashRestore puts items back where they came from.
//
// A link ID is only meaningful inside its volume, so the ids are matched against
// what is actually in the trash and restored per volume. An id that is in no
// trash is said so rather than sent, because Proton would answer a request about
// a volume it does not belong to by complaining about the request.
func (s *Service) TrashRestore(ctx context.Context, dc *Context, linkIDs []string) ([]Refused, error) {
	refs, err := s.TrashRefs(ctx, dc)
	if err != nil {
		return nil, err
	}
	volumeOf := make(map[string]string, len(refs))
	for _, ref := range refs {
		volumeOf[ref.LinkID] = ref.VolumeID
	}
	byVolume := map[string][]string{}
	order := []string{}
	for _, id := range linkIDs {
		volumeID, ok := volumeOf[id]
		if !ok {
			return nil, &errs.NotFound{Kind: "item in the trash", Ref: id}
		}
		if _, seen := byVolume[volumeID]; !seen {
			order = append(order, volumeID)
		}
		byVolume[volumeID] = append(byVolume[volumeID], id)
	}
	var refused []Refused
	for _, volumeID := range order {
		back, err := s.linkBatches(ctx, byVolume[volumeID], func(batch []string) proton.Request {
			return proton.Request{
				Method: "PUT", Path: "/drive/v2/volumes/" + volumeID + "/trash/restore_multiple",
				Body: map[string]any{"LinkIDs": batch},
			}
		})
		refused = append(refused, back...)
		if err != nil {
			return refused, err
		}
	}
	return refused, nil
}

// TrashEmpty empties the trash of every volume the account has, which is the set
// its listing counted.
func (s *Service) TrashEmpty(ctx context.Context, dc *Context) error {
	volumes, err := s.trashVolumes(ctx, dc)
	if err != nil {
		return err
	}
	for _, volumeID := range volumes {
		if err := s.C.Decode(ctx, proton.Request{
			Method: "DELETE", Path: "/drive/volumes/" + volumeID + "/trash",
		}, nil); err != nil {
			return err
		}
	}
	return nil
}

// chunk cuts ids into batches of at most n.
func chunk(ids []string, n int) [][]string {
	var out [][]string
	for start := 0; start < len(ids); start += n {
		out = append(out, ids[start:min(start+n, len(ids))])
	}
	return out
}
