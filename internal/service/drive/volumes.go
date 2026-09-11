package drive

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"time"

	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
	"github.com/roman-16/proton-cli/internal/account/keys"
	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/fetch"
	"github.com/roman-16/proton-cli/internal/proton"
	"github.com/roman-16/proton-cli/internal/skip"
	"github.com/roman-16/proton-cli/internal/units"
)

// The volumes an account's files and photos are kept on.
//
// There is one for files and, on a newer account, one for photos. A password
// reset locks them: the keys their shares were sealed to are shut, so Proton's
// clients make a fresh volume the next time Drive is opened and keep the old one
// aside, locked, until its owner either restores its files into the new one or
// has them deleted. Both are what Proton's Drive client offers on its banner, and
// both are here - as is making the fresh volume, which its client does the
// moment it loads.

// What Proton's volume records say. Mirrors VolumeType, VolumeState and
// VolumeRestoreStatus in WebClients (packages/shared/lib/interfaces/drive/volume.ts).
const (
	volumeFiles  = 1
	volumePhotos = 2

	volumeActive = 1
	volumeLocked = 3

	restoreDone     = 0
	restoreProgress = 1
	restoreFailed   = -1
)

// volumeRecord is a volume as Proton hands it back. The list names a volume as
// VolumeID and its share as ShareID; a volume just made names them ID and
// Share.ID.
type volumeRecord struct {
	ID            string
	VolumeID      string
	State         int
	Type          int
	UsedSpace     int64
	CreateTime    int64
	RestoreStatus *int
	Share         struct {
		ID      string
		ShareID string
		LinkID  string
	}
}

func (v volumeRecord) id() string {
	if v.VolumeID != "" {
		return v.VolumeID
	}
	return v.ID
}

func (v volumeRecord) shareID() string {
	if v.Share.ShareID != "" {
		return v.Share.ShareID
	}
	return v.Share.ID
}

func (v volumeRecord) volume() Volume {
	out := Volume{
		ID: v.id(), Type: "files", State: "active",
		UsedSpace: v.UsedSpace, Created: v.CreateTime,
	}
	if v.Type == volumePhotos {
		out.Type = "photos"
	}
	if v.State == volumeLocked {
		out.State = "locked"
	}
	if v.RestoreStatus != nil {
		switch *v.RestoreStatus {
		case restoreDone:
			out.Restore = "done"
		case restoreProgress:
			out.Restore = "in progress"
		case restoreFailed:
			out.Restore = "failed"
		}
	}
	return out
}

// Volume is one of the account's volumes as the CLI reports it.
type Volume struct {
	ID string `json:"id"`
	// Type is what the volume keeps: files, or photos.
	Type string `json:"type"`
	// State is active, or locked by a password reset.
	State     string `json:"state"`
	UsedSpace int64  `json:"used_space"`
	// Restore is how far a restore asked for earlier has got, and empty for a
	// volume nobody has asked to restore.
	Restore string `json:"restore,omitempty"`
	Created int64  `json:"created"`
}

// Locked reports whether a password reset locked the volume.
func (v Volume) Locked() bool { return v.State == "locked" }

// Photos reports whether the volume is the photo library's.
func (v Volume) Photos() bool { return v.Type == "photos" }

// NoVolume reports that the account has no active volume for its files: nothing
// has made one yet, or a password reset locked the one it had.
type NoVolume struct{}

func (*NoVolume) Error() string { return "Drive has no volume to work in yet." }
func (*NoVolume) ExitCode() int { return 1 }
func (*NoVolume) Hints() []string {
	return []string{"run the command without --dry-run, which makes one"}
}

// Volumes lists every volume the account has, locked ones included.
//
// A locked volume is found two ways, and both are read: the list of volumes,
// and the locked shares, each of which names the volume it is on. A volume
// only the shares name is read on its own.
func (s *Service) Volumes(ctx context.Context) ([]Volume, error) {
	var listed struct{ Volumes []volumeRecord }
	var shares []rawShare
	if err := fetch.Together(ctx,
		func(ctx context.Context) error {
			return s.C.Decode(ctx, proton.Request{Method: "GET", Path: "/drive/volumes"}, &listed)
		},
		func(ctx context.Context) error {
			var err error
			shares, err = s.fetchShares(ctx)
			return err
		},
	); err != nil {
		return nil, err
	}
	out := make([]Volume, 0, len(listed.Volumes))
	known := map[string]bool{}
	for _, v := range listed.Volumes {
		known[v.id()] = true
		out = append(out, v.volume())
	}
	for _, sh := range shares {
		if !sh.Locked || sh.VolumeID == "" || known[sh.VolumeID] {
			continue
		}
		known[sh.VolumeID] = true
		v, err := s.volume(ctx, sh.VolumeID)
		if err != nil {
			skip.Record(ctx, skip.KindVolume, sh.VolumeID, skip.Unreadable, err)
			continue
		}
		out = append(out, v)
	}
	return out, nil
}

// volume reads one volume by ID.
func (s *Service) volume(ctx context.Context, volumeID string) (Volume, error) {
	var r struct{ Volume volumeRecord }
	if err := s.C.Decode(ctx, proton.Request{Method: "GET", Path: "/drive/volumes/" + volumeID}, &r); err != nil {
		return Volume{}, err
	}
	return r.Volume.volume(), nil
}

// CreateVolume makes the volume an account keeps its files on, and opens it.
//
// It is what Proton's own client does the moment it finds no active files
// volume - on an account that has never opened Drive, and after a password
// reset locked the one there was. The keys are the primary address's, as its
// client makes them.
func (s *Service) CreateVolume(ctx context.Context) (*Context, error) {
	u, err := s.keys(ctx)
	if err != nil {
		return nil, err
	}
	rings, addr, err := u.PrimaryAddr()
	if err != nil {
		return nil, err
	}
	keyID, err := primaryKeyID(addr.Keys, addr.ID)
	if err != nil {
		return nil, err
	}
	shareKey, sharePass, sharePassSig, sharePriv, err := genNodeKeys(rings.Write, rings.Write)
	if err != nil {
		return nil, fmt.Errorf("make the volume's share key: %w", err)
	}
	shareKR, err := pgp.NewKeyRing(sharePriv)
	if err != nil {
		return nil, err
	}
	folderKey, folderPass, folderPassSig, folderPriv, err := genNodeKeys(shareKR, rings.Write)
	if err != nil {
		return nil, fmt.Errorf("make the volume's root key: %w", err)
	}
	folderKR, err := pgp.NewKeyRing(folderPriv)
	if err != nil {
		return nil, err
	}
	folderName, err := encryptName("root", shareKR, rings.Write)
	if err != nil {
		return nil, err
	}
	_, hashKey, err := genNodeHashKey(folderKR, folderKR)
	if err != nil {
		return nil, err
	}
	var r struct{ Volume volumeRecord }
	if err := s.C.Decode(ctx, proton.Request{
		Method: "POST", Path: "/drive/volumes",
		Body: map[string]any{
			"AddressID":                 addr.ID,
			"AddressKeyID":              keyID,
			"FolderHashKey":             hashKey,
			"FolderKey":                 folderKey,
			"FolderName":                folderName,
			"FolderPassphrase":          folderPass,
			"FolderPassphraseSignature": folderPassSig,
			"ShareKey":                  shareKey,
			"SharePassphrase":           sharePass,
			"SharePassphraseSignature":  sharePassSig,
		},
	}, &r); err != nil {
		return nil, err
	}
	slog.DebugContext(ctx, "drive: made the files volume", "volume", r.Volume.id())
	return s.unlockShare(ctx, r.Volume.shareID(), r.Volume.Share.LinkID, r.Volume.id())
}

// primaryKeyID is the record ID of the key an address writes with, which a
// request that names the signing key by ID rather than by fingerprint carries.
func primaryKeyID(records []keys.Key, addrID string) (string, error) {
	for _, k := range records {
		if k.Primary == 1 && k.Active != 0 {
			return k.ID, nil
		}
	}
	return "", fmt.Errorf("no primary key for address %s", addrID)
}

// lockedShare is a share of a locked volume, read far enough to be restored.
type lockedShare struct {
	shareID                    string
	kind                       int
	Passphrase                 string
	PassphraseSignature        string
	Key                        string
	PossibleKeyPackets         []struct{ KeyPacket string }
	RootLinkRecoveryPassphrase string
}

// opened is a locked share with its secrets in hand: the session key its
// passphrase is sealed under, the passphrase, and - for the share the files
// hang from - the passphrase of its root, which is what a restore re-seals.
type opened struct {
	shareID        string
	kind           int
	sessionKey     *pgp.SessionKey
	passphrase     []byte
	rootPassphrase []byte
}

// Restored is what a restore set in motion.
type Restored struct {
	// Folder is the folder the volume's files reappear in, and empty for a
	// volume that held no files of its own - computers and photos alone.
	Folder string `json:"folder,omitempty"`
	// Files, Computers and Photos count the shares handed back, by what they
	// keep.
	Files     int `json:"files"`
	Computers int `json:"computers"`
	Photos    int `json:"photos"`
}

// Restore hands the files of a locked volume back to the account, into the
// tree given.
//
// Files come back as a folder named for the moment, under the root of the tree;
// each computer and photo share is sealed to the address the tree belongs to
// instead, and stays where it is. Proton moves the files afterwards, in its own
// time. Every locked share of the volume has to open with a key of the
// account's, so a volume whose keys are still locked is refused before anything
// is sent.
func (s *Service) Restore(ctx context.Context, into *Context, volumeID string) (*Restored, error) {
	shares, err := s.fetchShares(ctx)
	if err != nil {
		return nil, err
	}
	u, err := s.keys(ctx)
	if err != nil {
		return nil, err
	}
	var open []opened
	for _, sh := range shares {
		if !sh.Locked || sh.VolumeID != volumeID {
			continue
		}
		if sh.Type != shareTypeMain && sh.Type != shareTypeDevice && sh.Type != shareTypePhotos {
			// Recorded and not counted: a share of another kind is not the
			// volume's own and is not what a restore hands back; Proton's clients
			// leave it out too.
			slog.DebugContext(ctx, "drive: a locked share is not the volume's own",
				"share", sh.ShareID, "volume", volumeID, "share_type", sh.Type)
			continue
		}
		locked, err := s.lockedShare(ctx, sh)
		if err != nil {
			return nil, err
		}
		o, err := openLocked(u, locked)
		if err != nil {
			slog.DebugContext(ctx, "drive: a locked share did not open",
				"share", sh.ShareID, "volume", volumeID, "error", err.Error())
			if len(u.Locked()) > 0 {
				return nil, errs.Problemf("The keys this volume was locked with are not active.").
					Hint("proton account keys reactivate")
			}
			return nil, fmt.Errorf("open locked share %s: %w", sh.ShareID, err)
		}
		open = append(open, *o)
	}
	if len(open) == 0 {
		return nil, errs.Problemf("Volume %s holds nothing to restore.", volumeID)
	}
	payload, restored, err := restorePayload(into, open, time.Now())
	if err != nil {
		return nil, err
	}
	if err := s.C.Decode(ctx, proton.Request{
		Method: "PUT", Path: "/drive/volumes/" + volumeID + "/restore", Body: payload,
	}, nil); err != nil {
		return nil, err
	}
	slog.DebugContext(ctx, "drive: asked for a volume to be restored",
		"volume", volumeID, "shares_files", restored.Files,
		"shares_computers", restored.Computers, "shares_photos", restored.Photos)
	return restored, nil
}

// lockedShare reads one locked share with the keys a restore needs.
func (s *Service) lockedShare(ctx context.Context, sh rawShare) (lockedShare, error) {
	var r lockedShare
	if err := s.C.Decode(ctx, proton.Request{Method: "GET", Path: "/drive/shares/" + sh.ShareID}, &r); err != nil {
		return lockedShare{}, err
	}
	r.shareID, r.kind = sh.ShareID, sh.Type
	return r, nil
}

// openLocked opens a locked share with whichever of the account's keys it was
// sealed to.
//
// Proton hands the session key packets over beside the share; the passphrase is
// opened with the session key one of them yields, checked against the
// account's own signature, and then opens the share key - which for the share
// the files hang from opens the root's passphrase in turn.
func openLocked(u *keys.Unlocked, sh lockedShare) (*opened, error) {
	msg, err := pgp.NewPGPMessageFromArmored(sh.Passphrase)
	if err != nil {
		return nil, fmt.Errorf("read the share passphrase: %w", err)
	}
	split, err := msg.SplitMessage()
	if err != nil {
		return nil, err
	}
	packets := split.GetBinaryKeyPacket()
	if len(sh.PossibleKeyPackets) > 0 {
		packets = nil
		for _, p := range sh.PossibleKeyPackets {
			raw, err := base64.StdEncoding.DecodeString(p.KeyPacket)
			if err != nil {
				return nil, fmt.Errorf("read a key packet: %w", err)
			}
			packets = append(packets, raw...)
		}
	}
	var sk *pgp.SessionKey
	for _, addr := range u.Addresses {
		rings, ok := u.AddrRings(addr.ID)
		if !ok {
			continue
		}
		if sk, err = rings.Read.DecryptSessionKey(packets); err == nil {
			break
		}
	}
	if sk == nil {
		return nil, fmt.Errorf("no key of the account's opens the share passphrase")
	}
	plain, err := sk.Decrypt(split.GetBinaryDataPacket())
	if err != nil {
		return nil, fmt.Errorf("decrypt the share passphrase: %w", err)
	}
	if err := signedByAccount(u, plain, sh.PassphraseSignature); err != nil {
		return nil, err
	}
	locked, err := pgp.NewKeyFromArmored(sh.Key)
	if err != nil {
		return nil, err
	}
	shareKey, err := locked.Unlock(plain.GetBinary())
	if err != nil {
		return nil, fmt.Errorf("unlock the share key: %w", err)
	}
	o := &opened{shareID: sh.shareID, kind: sh.kind, sessionKey: sk, passphrase: plain.GetBinary()}
	if sh.kind != shareTypeMain {
		return o, nil
	}
	if sh.RootLinkRecoveryPassphrase == "" {
		return nil, fmt.Errorf("the share carries no passphrase for its root")
	}
	shareKR, err := pgp.NewKeyRing(shareKey)
	if err != nil {
		return nil, err
	}
	root, err := pgp.NewPGPMessageFromArmored(sh.RootLinkRecoveryPassphrase)
	if err != nil {
		return nil, err
	}
	dec, err := shareKR.Decrypt(root, nil, pgp.GetUnixTime())
	if err != nil {
		return nil, fmt.Errorf("decrypt the root passphrase: %w", err)
	}
	o.rootPassphrase = dec.GetBinary()
	return o, nil
}

// signedByAccount checks that a passphrase was signed by one of the account's
// own keys, which is what says the share is the account's to restore.
func signedByAccount(u *keys.Unlocked, plain *pgp.PlainMessage, armoredSig string) error {
	sig, err := pgp.NewPGPSignatureFromArmored(armoredSig)
	if err != nil {
		return fmt.Errorf("read the share passphrase signature: %w", err)
	}
	text := pgp.NewPlainMessageFromString(string(plain.GetBinary()))
	for _, addr := range u.Addresses {
		rings, ok := u.AddrRings(addr.ID)
		if !ok {
			continue
		}
		if rings.Read.VerifyDetached(text, sig, pgp.GetUnixTime()) == nil {
			return nil
		}
	}
	return fmt.Errorf("the share passphrase is not signed by a key of the account's")
}

// restorePayload is what a restore hands Proton, sealed to the tree the files
// are going into: the folder each file share reappears as, and each computer
// and photo share re-sealed to the tree's address.
func restorePayload(into *Context, open []opened, now time.Time) (map[string]any, *Restored, error) {
	rootKR, err := into.RootKR()
	if err != nil {
		return nil, nil, err
	}
	keyID, err := primaryKeyID(into.addrKeys, into.AddrID)
	if err != nil {
		return nil, nil, err
	}
	restored := &Restored{}
	mains, devices, photos := []map[string]any{}, []map[string]any{}, []map[string]any{}
	for _, o := range open {
		switch o.kind {
		case shareTypeMain:
			name := restoredFolderName(now, restored.Files)
			entry, err := restoredFolder(into, rootKR, o, name)
			if err != nil {
				return nil, nil, err
			}
			mains = append(mains, entry)
			if restored.Files == 0 {
				restored.Folder = name
			}
			restored.Files++
		default:
			entry, err := resealedShare(into, o)
			if err != nil {
				return nil, nil, err
			}
			if o.kind == shareTypeDevice {
				devices = append(devices, entry)
				restored.Computers++
			} else {
				photos = append(photos, entry)
				restored.Photos++
			}
		}
	}
	return map[string]any{
		"AddressKeyID":     keyID,
		"Devices":          devices,
		"MainShares":       mains,
		"PhotoShares":      photos,
		"SignatureAddress": into.AddrEmail,
	}, restored, nil
}

// restoredFolderName is what the files of a restored volume come back under,
// named for the moment the way Proton's clients name it; a second file share
// of the same volume is numbered.
func restoredFolderName(now time.Time, index int) string {
	name := "Restored files " + units.Time(now.Unix())
	if index > 0 {
		name = fmt.Sprintf("%s (%d)", name, index+1)
	}
	return name
}

// restoredFolder makes a locked share's root a folder under the tree's root:
// the name sealed to the root and hashed under its hash key, the way any child
// of the root is named, and the root passphrase of the locked share sealed to
// the tree's root and signed as the tree's address.
func restoredFolder(into *Context, rootKR *pgp.KeyRing, o opened, name string) (map[string]any, error) {
	hashKey, err := hashKeyOf(into.rootLink, rootKR)
	if err != nil {
		return nil, err
	}
	hash, err := lookupHash(name, hashKey)
	if err != nil {
		return nil, err
	}
	encName, err := encryptName(name, rootKR, into.Addr.Write)
	if err != nil {
		return nil, err
	}
	passphrase, signature, err := sealPassphrase(string(o.rootPassphrase), rootKR, into.Addr.Write)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"Hash":                    hash,
		"LockedShareID":           o.shareID,
		"Name":                    encName,
		"NodePassphrase":          passphrase,
		"NodePassphraseSignature": signature,
	}, nil
}

// resealedShare hands a computer's or the photo library's share back under the
// tree's address: its session key sealed to that address's key, and its
// passphrase signed by it.
func resealedShare(into *Context, o opened) (map[string]any, error) {
	packet, err := into.Addr.Write.EncryptSessionKey(o.sessionKey)
	if err != nil {
		return nil, err
	}
	sig, err := into.Addr.Write.SignDetached(pgp.NewPlainMessageFromString(string(o.passphrase)))
	if err != nil {
		return nil, err
	}
	signature, err := sig.GetArmored()
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"LockedShareID":       o.shareID,
		"PassphraseSignature": signature,
		"ShareKeyPacket":      base64.StdEncoding.EncodeToString(packet),
	}, nil
}

// DeleteLocked has Proton delete a locked volume with everything on it, which
// its clients offer as the alternative to restoring one. The files go within
// seventy-two hours, and nothing brings them back.
func (s *Service) DeleteLocked(ctx context.Context, volumeID string) error {
	return s.C.Decode(ctx, proton.Request{
		Method: "PUT", Path: "/drive/volumes/" + volumeID + "/delete_locked",
	}, nil)
}
