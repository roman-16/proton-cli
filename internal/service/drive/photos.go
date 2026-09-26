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

	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
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

// photoTags maps the user-facing tag names to Proton's PhotoTag enum values
// (packages/shared/lib/interfaces/drive/file.ts). Proton assigns tags 1-9
// server-side by automatic classification; only Favorites (0) is user-
// togglable, via the dedicated favorite endpoint.
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

// favoriteTag is PhotoTag.Favorites, the only user-togglable tag.
const favoriteTag = 0

// TagName returns the user-facing name for a PhotoTag id, falling back to the
// decimal id for tags this build doesn't know so future backend tags still
// render legibly.
func TagName(id int) string {
	for _, t := range photoTags {
		if t.id == id {
			return t.name
		}
	}
	return strconv.Itoa(id)
}

// ParseTag resolves a user-supplied tag name to its PhotoTag id. Names only:
// integer input is rejected so the CLI never leaks raw enum values.
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

// tagNames maps PhotoTag ids to their names for display and JSON output.
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

// photosRoot unwraps the photos share root's node key ring, the parent for every
// photo and album.
func (s *Service) photosRoot(ctx context.Context, dc *Context) (*Link, *pgp.KeyRing, error) {
	kr, err := dc.RootKR()
	if err != nil {
		return nil, nil, err
	}
	return dc.rootLink, kr, nil
}

// PhotosList returns all photos on the photos volume (paginated server-side).
// When filter is set, the server filters to the single PhotoTag id in tag.
func (s *Service) PhotosList(ctx context.Context, dc *Context, tag int, filter bool) ([]Photo, error) {
	// The endpoint resumes from the last photo of the previous answer rather
	// than from a page number.
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

// AlbumsList returns the photo albums. The list endpoint omits the (encrypted)
// album name, so the links are read back together and their names decrypted
// with the photos-root key.
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

// AlbumItems lists the photos in an album.
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

// sharedAlbum is an album somebody shared with this account, as the photo
// library answers for it: which album it is, and the share it is held by.
type sharedAlbum struct {
	VolumeID string
	ShareID  string
	LinkID   string
	Locked   bool
}

// albumsSharedWithMe names the albums other people have shared with this
// account.
//
// Proton answers for them here rather than among the shares, so they are asked
// for separately and matched up by share ID with whatever the share listing
// held.
func (s *Service) albumsSharedWithMe(ctx context.Context) ([]sharedAlbum, error) {
	// The endpoint hands back the anchor its next answer starts from.
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

// albumPhoto is one photo an album holds, as the album's own endpoint answers:
// which photo it is and what the library knows about it, but nothing that would
// open it.
type albumPhoto struct {
	LinkID      string
	CaptureTime int64
	Hash        string
	Tags        []int
}

// albumPhotos names what an album holds.
func (s *Service) albumPhotos(ctx context.Context, dc *Context, albumLinkID string) ([]albumPhoto, error) {
	// The endpoint hands back the anchor its next answer starts from.
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

// albumChildren reads an album's photos as links, which is what a tree walk
// needs of them: the album endpoint says which photos are in it, and the links
// themselves carry the names and the keys.
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

// PhotoDownload streams and decrypts a photo by its link ID.
func (s *Service) PhotoDownload(ctx context.Context, dc *Context, linkID string, w io.Writer, opts DownloadOptions) (string, error) {
	res, err := s.ResolveRef(ctx, dc, linkID)
	if err != nil {
		return "", err
	}
	name := res.Name
	if name == "" {
		name = linkID
	}
	link := res.Link
	return name, s.downloadFile(ctx, dc, link, res.NodeKR, activeRevisionID(link), link.Size, w, opts)
}

// PhotoPlan is what uploading a photo will do, worked out before it does it:
// nothing at all when the library already holds the photo.
type PhotoPlan struct {
	// Duplicate is the photo the library already holds under the same name and
	// with the same content, and empty when it holds none.
	Duplicate string

	upload  *UploadPlan
	hashKey []byte
	open    func() (io.ReadCloser, error)
}

// PlanPhotoUpload decides what uploading the photo open reads, under name, will
// do.
//
// A photo is a duplicate when the library holds one of the same name and the
// same content, which is the test Proton's own apps apply: cameras reuse names,
// so a name alone says nothing. The content is read for the comparison only
// when the name matches.
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

// linkActive is the state of an item that is neither a draft nor in the trash.
const linkActive = 1

// photoDuplicate names the photo in the library that has name and the content
// open reads, or nothing.
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

// contentDigest is the SHA-1 of what open reads, as the hex a revision records.
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

// PhotoUpload writes the photo a plan describes into the library, as taken at
// captureTime (Unix seconds). A plan that found the photo already there writes
// nothing.
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

// AlbumCreate creates a new (unlocked) photo album and returns its link ID.
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

// AlbumRename changes what an album is called.
//
// An album is a link in the photo volume, so it renames the way anything else
// does: the new name encrypted to the volume root's key, and a hash of it so
// Proton can refuse a name the root already holds without reading either.
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

// AlbumAddPhotos adds existing timeline photos to an album. Each photo's node
// passphrase and name are re-encrypted to the album's node key (the same
// re-wrap used by Copy), and a fresh name hash is computed against the album's
// hash key.
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
	// The album-add payload requires each photo's ContentHash; reuse the photo's
	// existing one (the web client's documented fallback).
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

// AlbumRemovePhotos removes photos from an album (the photos themselves remain
// on the timeline).
func (s *Service) AlbumRemovePhotos(ctx context.Context, dc *Context, albumLinkID string, linkIDs []string) error {
	return s.C.Decode(ctx, proton.Request{
		Method: "POST", Path: fmt.Sprintf("/drive/photos/volumes/%s/albums/%s/remove-multiple", dc.VolumeID, albumLinkID),
		Body: map[string]any{"LinkIDs": linkIDs},
	}, nil)
}

// PhotosDelete moves photos to the trash. When permanent is set, the photos are
// then purged from it.
//
// A photo library is the largest collection an account has, so it goes through
// the same batching every other bulk change does: the photo volume's trash is
// the file tree's trash under another volume ID.
func (s *Service) PhotosDelete(ctx context.Context, dc *Context, linkIDs []string, permanent bool) ([]Refused, error) {
	if permanent {
		return s.Delete(ctx, dc, removing(linkIDs))
	}
	return s.Trash(ctx, dc, linkIDs)
}

// AlbumDelete deletes an album. When deletePhotos is true the album's photos
// are trashed too; otherwise they remain on the timeline.
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

// PhotoTagsRemove removes classification tags from a photo.
func (s *Service) PhotoTagsRemove(ctx context.Context, dc *Context, linkID string, tags []int) error {
	return s.C.Decode(ctx, proton.Request{
		Method: "DELETE", Path: fmt.Sprintf("/drive/photos/volumes/%s/links/%s/tags", dc.VolumeID, linkID),
		Body: map[string]any{"Tags": tags},
	}, nil)
}

// PhotosFavorite marks photos as favorite (PhotoTag.Favorites). A photo already
// in the timeline (parent == photos root) is tagged in place with an empty
// body. A photo that lives only in an album is copied into the timeline first:
// its name and node passphrase are re-encrypted to the photos root (along with
// any related burst/live photos) and sent as PhotoData, mirroring the web
// client's favorite-with-copy flow. It returns how many photos were copied.
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

// PhotosUnfavorite removes the Favorites tag from photos (DELETE .../tags).
func (s *Service) PhotosUnfavorite(ctx context.Context, dc *Context, linkIDs []string) error {
	for _, id := range linkIDs {
		if err := s.PhotoTagsRemove(ctx, dc, id, []int{favoriteTag}); err != nil {
			return err
		}
	}
	return nil
}

// buildFavoritePhotoData builds the PhotoData body that copies an album-only
// photo (and any related burst/live photos) into the timeline while favoriting
// it: each photo's name and node passphrase are re-encrypted from its current
// parent to the photos root.
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

// favoriteMovedParams re-encrypts a single photo's name + node passphrase from
// its current parent to the photos root and returns the fields the favorite
// endpoint's PhotoData expects.
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

// photoParentKR returns the key ring that wraps a photo's node passphrase and
// name: the photos-root key ring for a photo in the timeline, and the album's
// for one that lives only in an album.
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

// photoContentHash returns the photo's content hash (HMAC of its SHA-1 digest
// under the photos-root hash key, used for duplicate detection). It prefers the
// value the API already stores on the revision, falling back to recomputing it
// from the node's XAttr SHA-1 digest.
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

// AlbumSetCover chooses which of an album's photos represents it.
//
// The cover is a plain reference to a photo already in the album, so nothing is
// re-encrypted and nothing moves: it is the album saying which of its own
// children to show.
func (s *Service) AlbumSetCover(ctx context.Context, dc *Context, albumLinkID, photoLinkID string) error {
	photos, err := s.AlbumItems(ctx, dc, albumLinkID)
	if err != nil {
		return err
	}
	// A cover that is not in the album would be a reference the album cannot
	// resolve, so it is refused here rather than stored and shown as a gap.
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
