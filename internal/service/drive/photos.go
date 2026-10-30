package drive

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
	"time"

	pgp "github.com/ProtonMail/gopenpgp/v3/crypto"
	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/proton"
)

type Photo struct {
	LinkID      string   `json:"link_id"`
	CaptureTime int64    `json:"capture_time"`
	Hash        string   `json:"hash,omitempty"`
	ContentHash string   `json:"content_hash,omitempty"`
	Tags        []string `json:"tags"`
}

type Album struct {
	LinkID     string `json:"link_id"`
	Name       string `json:"name"`
	PhotoCount int    `json:"photo_count"`

	hash string
}

const photosPageSize = 500

var photoTags = []struct {
	name string
	id   int
}{
	{"favorites", 0},
	{"screenshots", 1},
	{"videos", 2},
	{"live-photos", 3},
	{"motion-photos", 4},
	{"selfies", 5},
	{"portraits", 6},
	{"bursts", 7},
	{"panoramas", 8},
	{"raw", 9},
}

const favoriteTag = 0

func TagName(id int) string {
	for _, t := range photoTags {
		if t.id == id {
			return t.name
		}
	}
	return strconv.Itoa(id)
}

func ParseTag(name string) (int, error) {
	for _, t := range photoTags {
		if t.name == name {
			return t.id, nil
		}
	}
	names := make([]string, len(photoTags))
	for i, t := range photoTags {
		names[i] = t.name
	}
	return 0, fmt.Errorf("unknown tag %q; valid: %s", name, strings.Join(names, ", "))
}

func tagNames(ids []int) []string {
	if len(ids) == 0 {
		return nil
	}
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = TagName(id)
	}
	return out
}

func (s *Service) photosRoot(ctx context.Context, dc *Context) (*Link, *pgp.KeyRing, error) {
	kr, err := dc.RootKR()
	if err != nil {
		return nil, nil, err
	}
	return dc.rootLink, kr, nil
}

func (s *Service) PhotosList(ctx context.Context, dc *Context, tag int, filter bool) ([]Photo, error) {
	lastID := ""
	return proton.All(ctx, func(ctx context.Context, _ int) ([]Photo, bool, error) {
		q := proton.Request{Method: "GET", Path: fmt.Sprintf("/drive/volumes/%s/photos", dc.VolumeID)}
		q.Query = map[string][]string{"PageSize": {fmt.Sprintf("%d", photosPageSize)}}
		if filter {
			q.Query.Set("Tag", strconv.Itoa(tag))
		}
		if lastID != "" {
			q.Query.Set("PreviousPageLastLinkID", lastID)
		}
		var r struct {
			Photos []struct {
				LinkID      string
				CaptureTime int64
				Hash        string
				ContentHash string
				Tags        []int
			}
		}
		if err := s.C.Decode(ctx, q, &r); err != nil {
			return nil, false, err
		}
		out := make([]Photo, 0, len(r.Photos))
		for _, p := range r.Photos {
			out = append(out, Photo{LinkID: p.LinkID, CaptureTime: p.CaptureTime, Hash: p.Hash, ContentHash: p.ContentHash, Tags: tagNames(p.Tags)})
		}
		if !proton.Full(out, photosPageSize) {
			return out, false, nil
		}
		lastID = out[len(out)-1].LinkID
		return out, true, nil
	})
}

func (s *Service) AlbumsList(ctx context.Context, dc *Context) ([]Album, error) {
	_, rootKR, err := s.photosRoot(ctx, dc)
	if err != nil {
		return nil, err
	}
	var r struct {
		Albums []struct {
			LinkID     string
			PhotoCount int
		}
	}
	if err := s.C.Decode(ctx, proton.Request{Method: "GET", Path: fmt.Sprintf("/drive/photos/volumes/%s/albums", dc.VolumeID)}, &r); err != nil {
		return nil, err
	}
	counts := make(map[string]int, len(r.Albums))
	ids := make([]string, 0, len(r.Albums))
	for _, a := range r.Albums {
		counts[a.LinkID] = a.PhotoCount
		ids = append(ids, a.LinkID)
	}
	out := make([]Album, 0, len(ids))
	for _, batch := range chunk(ids, linkBatch) {
		links, err := s.linkMetadata(ctx, dc.ShareID, batch)
		if err != nil {
			return nil, err
		}
		for _, link := range links {
			name, err := decryptName(link.Name, rootKR)
			if err != nil {
				// Recorded and not counted: the album is in the listing, addressable
				// by the ID beside it, and the empty name is the screen saying so.
				slog.DebugContext(ctx, "drive: an album's name could not be decrypted",
					"link", link.LinkID, "share", dc.ShareID, "error", err)
			}
			out = append(out, Album{LinkID: link.LinkID, Name: name, PhotoCount: counts[link.LinkID], hash: link.Hash})
		}
	}
	return out, nil
}

func (s *Service) AlbumItems(ctx context.Context, dc *Context, albumLinkID string) ([]Photo, error) {
	entries, err := s.albumPhotos(ctx, dc, albumLinkID)
	if err != nil {
		return nil, err
	}
	out := make([]Photo, 0, len(entries))
	for _, p := range entries {
		out = append(out, Photo{LinkID: p.LinkID, CaptureTime: p.CaptureTime, Hash: p.Hash, Tags: tagNames(p.Tags)})
	}
	return out, nil
}

type sharedAlbum struct {
	VolumeID string
	ShareID  string
	LinkID   string
	Locked   bool
}

func (s *Service) albumsSharedWithMe(ctx context.Context) ([]sharedAlbum, error) {
	anchor := ""
	return proton.All(ctx, func(ctx context.Context, _ int) ([]sharedAlbum, bool, error) {
		q := proton.Request{Method: "GET", Path: "/drive/photos/albums/shared-with-me"}
		if anchor != "" {
			q.Query = map[string][]string{"AnchorID": {anchor}}
		}
		var r struct {
			Albums   []sharedAlbum
			AnchorID string
			More     bool
		}
		if err := s.C.Decode(ctx, q, &r); err != nil {
			return nil, false, err
		}
		anchor = r.AnchorID
		return r.Albums, r.More && r.AnchorID != "", nil
	})
}

type albumPhoto struct {
	LinkID      string
	CaptureTime int64
	Hash        string
	Tags        []int
}

func (s *Service) albumPhotos(ctx context.Context, dc *Context, albumLinkID string) ([]albumPhoto, error) {
	anchor := ""
	return proton.All(ctx, func(ctx context.Context, _ int) ([]albumPhoto, bool, error) {
		q := proton.Request{Method: "GET", Path: fmt.Sprintf("/drive/photos/volumes/%s/albums/%s/children", dc.VolumeID, albumLinkID)}
		if anchor != "" {
			q.Query = map[string][]string{"AnchorID": {anchor}}
		}
		var r struct {
			Photos   []albumPhoto
			AnchorID string
			More     bool
		}
		if err := s.C.Decode(ctx, q, &r); err != nil {
			return nil, false, err
		}
		anchor = r.AnchorID
		return r.Photos, r.More && r.AnchorID != "", nil
	})
}

func (s *Service) albumChildren(ctx context.Context, dc *Context, albumLinkID string) ([]Link, error) {
	entries, err := s.albumPhotos(ctx, dc, albumLinkID)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(entries))
	for _, p := range entries {
		ids = append(ids, p.LinkID)
	}
	out := make([]Link, 0, len(ids))
	for _, batch := range chunk(ids, linkBatch) {
		links, err := s.linkMetadata(ctx, dc.ShareID, batch)
		if err != nil {
			return nil, err
		}
		out = append(out, links...)
	}
	return out, nil
}

func (s *Service) PhotoDownload(ctx context.Context, dc *Context, linkID string, w io.Writer, opts DownloadOptions) (string, time.Time, error) {
	res, err := s.ResolveRef(ctx, dc, linkID)
	if err != nil {
		return "", time.Time{}, err
	}
	name := res.Name
	if name == "" {
		name = linkID
	}
	link := res.Link
	return name, res.Modified(), s.downloadFile(ctx, dc, link, res.NodeKR, activeRevisionID(link), link.Size, w, opts)
}

type PhotoPlan struct {
	Duplicate string

	upload  *UploadPlan
	hashKey []byte
	open    func() (io.ReadCloser, error)
}

func (s *Service) PlanPhotoUpload(ctx context.Context, dc *Context, name string, open func() (io.ReadCloser, error)) (*PhotoPlan, error) {
	root, rootKR, err := s.photosRoot(ctx, dc)
	if err != nil {
		return nil, err
	}
	hashKey, err := hashKeyOf(root, rootKR)
	if err != nil {
		return nil, err
	}
	duplicate, err := s.photoDuplicate(ctx, dc, name, hashKey, open)
	if err != nil {
		return nil, err
	}
	if duplicate != "" {
		return &PhotoPlan{Duplicate: duplicate}, nil
	}
	upload, err := s.PlanUpload(ctx, dc, "/", name, ConflictRefuse)
	if err != nil {
		return nil, err
	}
	return &PhotoPlan{upload: upload, hashKey: hashKey, open: open}, nil
}

const linkActive = 1

func (s *Service) photoDuplicate(ctx context.Context, dc *Context, name string, hashKey []byte, open func() (io.ReadCloser, error)) (string, error) {
	nameHash, err := lookupHash(name, hashKey)
	if err != nil {
		return "", err
	}
	var r struct {
		DuplicateHashes []struct {
			Hash        string
			ContentHash string
			LinkState   int
			LinkID      string
		}
	}
	if err := s.C.Decode(ctx, proton.Request{
		Method: "POST", Reads: true,
		Path: fmt.Sprintf("/drive/volumes/%s/photos/duplicates", dc.VolumeID),
		Body: map[string]any{"NameHashes": []string{nameHash}},
	}, &r); err != nil {
		return "", err
	}
	candidates := map[string]string{}
	for _, d := range r.DuplicateHashes {
		if d.Hash == nameHash && d.LinkState == linkActive && d.LinkID != "" && d.ContentHash != "" {
			candidates[d.ContentHash] = d.LinkID
		}
	}
	if len(candidates) == 0 {
		return "", nil
	}
	digest, err := contentDigest(open)
	if err != nil {
		return "", err
	}
	contentHash, err := lookupHash(digest, hashKey)
	if err != nil {
		return "", err
	}
	return candidates[contentHash], nil
}

func contentDigest(open func() (io.ReadCloser, error)) (string, error) {
	r, err := open()
	if err != nil {
		return "", err
	}
	defer func() { _ = r.Close() }()
	digest := sha1.New() //nolint:gosec // Proton records the content digest as SHA-1
	if _, err := io.Copy(digest, r); err != nil {
		return "", err
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func (s *Service) PhotoUpload(ctx context.Context, dc *Context, plan *PhotoPlan, captureTime int64, opts UploadOptions) error {
	if plan.Duplicate != "" {
		return nil
	}
	r, err := plan.open()
	if err != nil {
		return err
	}
	defer func() { _ = r.Close() }()
	opts.photo = &photoRevision{captureTime: captureTime, hashKey: plan.hashKey}
	return s.Upload(ctx, dc, plan.upload, r, opts)
}

func (s *Service) AlbumCreate(ctx context.Context, dc *Context, name string) (string, error) {
	root, rootKR, err := s.photosRoot(ctx, dc)
	if err != nil {
		return "", err
	}
	hashKey, err := hashKeyOf(root, rootKR)
	if err != nil {
		return "", err
	}
	hash, err := lookupHash(name, hashKey)
	if err != nil {
		return "", err
	}
	encName, err := encryptName(name, rootKR, dc.Addr.Write)
	if err != nil {
		return "", err
	}
	nodeKey, nodePass, nodePassSig, nodePriv, err := genNodeKeys(rootKR, dc.Addr.Write)
	if err != nil {
		return "", err
	}
	nodeKR, err := pgp.NewKeyRing(nodePriv)
	if err != nil {
		return "", err
	}
	_, nodeHashKey, err := genNodeHashKey(nodeKR, nodeKR)
	if err != nil {
		return "", err
	}
	var r struct {
		Album struct{ Link struct{ LinkID string } }
	}
	if err := s.C.Decode(ctx, proton.Request{
		Method: "POST", Path: fmt.Sprintf("/drive/photos/volumes/%s/albums", dc.VolumeID),
		Body: map[string]any{
			"Locked": false,
			"Link": map[string]any{
				"Name": encName, "Hash": hash,
				"NodePassphrase": nodePass, "NodePassphraseSignature": nodePassSig,
				"SignatureEmail": dc.AddrEmail,
				"NodeKey":        nodeKey, "NodeHashKey": nodeHashKey,
			},
		},
	}, &r); err != nil {
		return "", err
	}
	return r.Album.Link.LinkID, nil
}

func (s *Service) AlbumRename(ctx context.Context, dc *Context, album Album, newName string) error {
	root, rootKR, err := s.photosRoot(ctx, dc)
	if err != nil {
		return err
	}
	hashKey, err := hashKeyOf(root, rootKR)
	if err != nil {
		return err
	}
	newHash, err := lookupHash(newName, hashKey)
	if err != nil {
		return err
	}
	encName, err := encryptName(newName, rootKR, dc.Addr.Write)
	if err != nil {
		return err
	}
	err = s.C.Decode(ctx, proton.Request{
		Method: "PUT",
		Path:   fmt.Sprintf("/drive/shares/%s/links/%s/rename", dc.ShareID, album.LinkID),
		Body: map[string]any{
			"Name": encName, "Hash": newHash, "OriginalHash": album.hash,
			"NameSignatureEmail": dc.AddrEmail,
		},
	}, nil)
	if proton.AlreadyExists(err) {
		return &errs.Exists{Kind: "album", Name: newName, Where: "your photos"}
	}
	return err
}

func (s *Service) AlbumAddPhotos(ctx context.Context, dc *Context, albumLinkID string, photoLinkIDs []string) error {
	_, rootKR, err := s.photosRoot(ctx, dc)
	if err != nil {
		return err
	}
	albumLink, err := s.getLink(ctx, dc.ShareID, albumLinkID)
	if err != nil {
		return err
	}
	albumKR, err := unlockNode(albumLink, rootKR, dc.Addr.Read)
	if err != nil {
		return fmt.Errorf("unlock album: %w", err)
	}
	albumHashKey, err := hashKeyOf(albumLink, albumKR)
	if err != nil {
		return err
	}
	contentHashes := map[string]string{}
	photos, err := s.PhotosList(ctx, dc, 0, false)
	if err != nil {
		return err
	}
	for _, p := range photos {
		contentHashes[p.LinkID] = p.ContentHash
	}
	var data []map[string]any
	for _, pid := range photoLinkIDs {
		photoLink, err := s.getLink(ctx, dc.ShareID, pid)
		if err != nil {
			return err
		}
		name, err := decryptName(photoLink.Name, rootKR)
		if err != nil {
			return fmt.Errorf("decrypt photo name %s: %w", pid, err)
		}
		encName, err := reEncryptName(photoLink.Name, name, rootKR, albumKR, dc.Addr.Write)
		if err != nil {
			return err
		}
		hash, err := lookupHash(name, albumHashKey)
		if err != nil {
			return err
		}
		newPass, _, err := reEncryptNodePassphrase(photoLink, rootKR, albumKR, dc.Addr.Write)
		if err != nil {
			return fmt.Errorf("re-encrypt passphrase %s: %w", pid, err)
		}
		item := map[string]any{
			"LinkID": pid, "Name": encName, "Hash": hash,
			"NodePassphrase": newPass, "NameSignatureEmail": dc.AddrEmail,
		}
		if ch := contentHashes[pid]; ch != "" {
			item["ContentHash"] = ch
		}
		data = append(data, item)
	}
	return s.C.Decode(ctx, proton.Request{
		Method: "POST", Path: fmt.Sprintf("/drive/photos/volumes/%s/albums/%s/add-multiple", dc.VolumeID, albumLinkID),
		Body: map[string]any{"AlbumData": data},
	}, nil)
}

func (s *Service) AlbumRemovePhotos(ctx context.Context, dc *Context, albumLinkID string, linkIDs []string) error {
	return s.C.Decode(ctx, proton.Request{
		Method: "POST", Path: fmt.Sprintf("/drive/photos/volumes/%s/albums/%s/remove-multiple", dc.VolumeID, albumLinkID),
		Body: map[string]any{"LinkIDs": linkIDs},
	}, nil)
}

func (s *Service) PhotosDelete(ctx context.Context, dc *Context, linkIDs []string, permanent bool) ([]Refused, error) {
	if permanent {
		return s.Delete(ctx, dc, removing(linkIDs))
	}
	return s.Trash(ctx, dc, linkIDs)
}

func (s *Service) AlbumDelete(ctx context.Context, dc *Context, albumLinkID string, deletePhotos bool) error {
	q := url.Values{}
	if deletePhotos {
		q.Set("DeleteAlbumPhotos", "1")
	} else {
		q.Set("DeleteAlbumPhotos", "0")
	}
	return s.C.Decode(ctx, proton.Request{
		Method: "DELETE", Path: fmt.Sprintf("/drive/photos/volumes/%s/albums/%s", dc.VolumeID, albumLinkID),
		Query: q,
	}, nil)
}

func (s *Service) PhotoTagsRemove(ctx context.Context, dc *Context, linkID string, tags []int) error {
	return s.C.Decode(ctx, proton.Request{
		Method: "DELETE", Path: fmt.Sprintf("/drive/photos/volumes/%s/links/%s/tags", dc.VolumeID, linkID),
		Body: map[string]any{"Tags": tags},
	}, nil)
}

func (s *Service) PhotosFavorite(ctx context.Context, dc *Context, linkIDs []string) (copied int, err error) {
	root, rootKR, err := s.photosRoot(ctx, dc)
	if err != nil {
		return 0, err
	}
	rootHashKey, err := hashKeyOf(root, rootKR)
	if err != nil {
		return 0, err
	}
	for _, id := range linkIDs {
		link, err := s.getLink(ctx, dc.ShareID, id)
		if err != nil {
			return copied, err
		}
		body := map[string]any{}
		if link.ParentLinkID != root.LinkID {
			pd, err := s.buildFavoritePhotoData(ctx, dc, link, rootKR, rootHashKey)
			if err != nil {
				return copied, fmt.Errorf("favorite %s: %w", id, err)
			}
			body = pd
			copied++
		}
		if err := s.C.Decode(ctx, proton.Request{
			Method: "POST", Path: fmt.Sprintf("/drive/photos/volumes/%s/links/%s/favorite", dc.VolumeID, id),
			Body: body,
		}, nil); err != nil {
			return copied, err
		}
	}
	return copied, nil
}

func (s *Service) PhotosUnfavorite(ctx context.Context, dc *Context, linkIDs []string) error {
	for _, id := range linkIDs {
		if err := s.PhotoTagsRemove(ctx, dc, id, []int{favoriteTag}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) buildFavoritePhotoData(ctx context.Context, dc *Context, link *Link, rootKR *pgp.KeyRing, rootHashKey []byte) (map[string]any, error) {
	main, err := s.favoriteMovedParams(ctx, dc, link, rootKR, rootHashKey)
	if err != nil {
		return nil, err
	}
	related := []map[string]any{}
	if link.FileProperties != nil {
		for _, rid := range link.FileProperties.ActiveRevision.Photo.RelatedPhotosLinkIDs {
			relatedLink, err := s.getLink(ctx, dc.ShareID, rid)
			if err != nil {
				return nil, err
			}
			rp, err := s.favoriteMovedParams(ctx, dc, relatedLink, rootKR, rootHashKey)
			if err != nil {
				return nil, fmt.Errorf("related photo %s: %w", rid, err)
			}
			rp["LinkID"] = rid
			related = append(related, rp)
		}
	}
	main["RelatedPhotos"] = related
	return map[string]any{"PhotoData": main}, nil
}

func (s *Service) favoriteMovedParams(ctx context.Context, dc *Context, link *Link, rootKR *pgp.KeyRing, rootHashKey []byte) (map[string]any, error) {
	parentKR, err := s.photoParentKR(ctx, dc, link, rootKR)
	if err != nil {
		return nil, err
	}
	name, err := decryptName(link.Name, parentKR)
	if err != nil {
		return nil, fmt.Errorf("decrypt photo name %s: %w", link.LinkID, err)
	}
	encName, err := reEncryptName(link.Name, name, parentKR, rootKR, dc.Addr.Write)
	if err != nil {
		return nil, err
	}
	hash, err := lookupHash(name, rootHashKey)
	if err != nil {
		return nil, err
	}
	newPass, _, err := reEncryptNodePassphrase(link, parentKR, rootKR, dc.Addr.Write)
	if err != nil {
		return nil, fmt.Errorf("re-encrypt passphrase %s: %w", link.LinkID, err)
	}
	contentHash, err := s.photoContentHash(link, parentKR, rootHashKey)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"Name": encName, "Hash": hash, "ContentHash": contentHash,
		"NameSignatureEmail": dc.AddrEmail, "NodePassphrase": newPass,
	}, nil
}

func (s *Service) photoParentKR(ctx context.Context, dc *Context, link *Link, rootKR *pgp.KeyRing) (*pgp.KeyRing, error) {
	albumID := heldBy(link, dc.RootLinkID)
	if albumID == "" {
		return rootKR, nil
	}
	album, err := s.ResolveRef(ctx, dc, albumID)
	if err != nil {
		return nil, fmt.Errorf("open the album %s is in: %w", link.LinkID, err)
	}
	return album.NodeKR, nil
}

func (s *Service) photoContentHash(link *Link, parentKR *pgp.KeyRing, rootHashKey []byte) (string, error) {
	if link.FileProperties != nil && link.FileProperties.ActiveRevision.Photo.ContentHash != "" {
		return link.FileProperties.ActiveRevision.Photo.ContentHash, nil
	}
	nodeKR, err := unlockNode(link, parentKR, nil)
	if err != nil {
		return "", err
	}
	x, err := decryptXAttr(link.XAttr, nodeKR)
	if err != nil || x.Common.Digests.SHA1 == "" {
		return "", fmt.Errorf("photo %s has no content hash and no SHA-1 digest to recompute it", link.LinkID)
	}
	return lookupHash(x.Common.Digests.SHA1, rootHashKey)
}

func (s *Service) AlbumSetCover(ctx context.Context, dc *Context, albumLinkID, photoLinkID string) error {
	photos, err := s.AlbumItems(ctx, dc, albumLinkID)
	if err != nil {
		return err
	}
	for _, p := range photos {
		if p.LinkID == photoLinkID {
			return s.C.Decode(ctx, proton.Request{
				Method: "PUT",
				Path:   fmt.Sprintf("/drive/photos/volumes/%s/albums/%s", dc.VolumeID, albumLinkID),
				Body:   map[string]any{"CoverLinkID": photoLinkID},
			}, nil)
		}
	}
	return errs.Problemf("that photo is not in the album.").
		Hint("`drive photos list --album " + albumLinkID + "` shows what is.").Exit(3)
}
