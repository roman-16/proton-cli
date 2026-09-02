package pass

import (
	"context"
	"encoding/base64"
	"fmt"
	"sort"
	"time"

	"github.com/roman-16/proton-cli/internal/crypto/aead"
	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/proton"
)

// Secure links: a URL that shows one item to somebody with no Proton account.
//
// The item stays encrypted. A fresh key is made for the link, the item's own key
// is sealed under it, and that link key is what goes in the URL - after the '#',
// so it is never sent to Proton by a browser following it. Proton holds the
// sealed item key and hands it to whoever presents the link, and only somebody
// with the whole URL can open it.
//
// That is also why the URL is worth treating as the secret it is: anyone who
// sees it can read the item until the link expires or is revoked.

// SecureLink is a link somebody has been given.
type SecureLink struct {
	LinkID  string `json:"link_id"`
	ShareID string `json:"share_id"`
	ItemID  string `json:"item_id"`
	// URL is the whole link, key included. It is only known where the link key
	// can be unsealed, which is any item this account can read.
	URL string `json:"url,omitempty"`
	// Active is false once a link has expired or been read its last time.
	Active bool `json:"active"`
	// Expires is when it stops working, as a Unix time.
	Expires int64 `json:"expires,omitempty"`
	// Reads is how many times it has been opened, and MaxReads how many times it
	// may be. MaxReads is zero for a link with no limit.
	Reads    int `json:"reads"`
	MaxReads int `json:"max_reads,omitempty"`

	// What the URL is rebuilt from, which only a command that hands one over does.
	url         string
	sealedKey   string
	withItemKey bool
}

// NewSecureLink is what to make.
type NewSecureLink struct {
	// Expires is how long the link lasts. Proton requires one.
	Expires time.Duration
	// MaxReads caps how many times it may be opened, or zero for no cap.
	MaxReads int
}

// SecureLinkCreate makes a link to one item and returns it whole.
//
// The returned URL is the only time the link key is in hand: Proton stores it
// sealed, so a listing can rebuild it, but nothing else can.
func (s *Service) SecureLinkCreate(ctx context.Context, shareID, itemID string, opts NewSecureLink) (*SecureLink, error) {
	sk, err := s.decryptShareKeys(ctx, shareID)
	if err != nil {
		return nil, err
	}
	itemKey, rotation, err := s.latestItemKey(ctx, sk, shareID, itemID)
	if err != nil {
		return nil, err
	}
	revision, err := s.itemRevision(ctx, shareID, itemID)
	if err != nil {
		return nil, err
	}

	// A key of its own for the link, so revoking one link tells nothing about
	// the item's key or about any other link to it.
	linkKey, err := aead.NewKey()
	if err != nil {
		return nil, err
	}
	sealedItemKey, err := aead.Encrypt(linkKey, itemKey, []byte(aead.TagItemKey))
	if err != nil {
		return nil, err
	}
	sealedLinkKey, err := aead.Encrypt(itemKey, linkKey, []byte(aead.TagLinkKey))
	if err != nil {
		return nil, err
	}

	body := map[string]any{
		"Revision":                    revision,
		"EncryptedItemKey":            base64.StdEncoding.EncodeToString(sealedItemKey),
		"EncryptedLinkKey":            base64.StdEncoding.EncodeToString(sealedLinkKey),
		"ExpirationTime":              int(opts.Expires.Seconds()),
		"LinkKeyShareKeyRotation":     rotation,
		"LinkKeyEncryptedWithItemKey": true,
	}
	if opts.MaxReads > 0 {
		body["MaxReadCount"] = opts.MaxReads
	}

	var r struct {
		PublicLink struct {
			PublicLinkID   string
			Url            string
			ExpirationTime int64
		}
	}
	if err := s.C.Decode(ctx, proton.Request{
		Method: "POST", Path: "/pass/v1/share/" + shareID + "/item/" + itemID + "/public_link",
		Body: body,
	}, &r); err != nil {
		return nil, err
	}
	return &SecureLink{
		LinkID: r.PublicLink.PublicLinkID, ShareID: shareID, ItemID: itemID,
		URL:     linkURL(r.PublicLink.Url, linkKey),
		Active:  true,
		Expires: r.PublicLink.ExpirationTime, MaxReads: opts.MaxReads,
	}, nil
}

// linkURL puts the key in the fragment, which is where Proton's own clients put
// it: a browser never sends a fragment to the server, so the key stays between
// the person who made the link and the person given it.
func linkURL(url string, linkKey []byte) string {
	return url + "#" + base64.RawURLEncoding.EncodeToString(linkKey)
}

// SecureLinkGet reads one link back, URL and all.
//
// Proton stores the link key sealed under the item's own, so the whole URL can
// be rebuilt - which is what makes a link somebody mislaid recoverable rather
// than something to revoke and make again.
func (s *Service) SecureLinkGet(ctx context.Context, linkID string) (*SecureLink, error) {
	links, err := s.secureLinks(ctx)
	if err != nil {
		return nil, err
	}
	for _, l := range links {
		if l.LinkID != linkID {
			continue
		}
		if url, err := s.rebuildLink(ctx, l.ShareID, l.ItemID, l.url, l.sealedKey, l.withItemKey); err == nil {
			l.URL = url
		}
		return &l, nil
	}
	return nil, &errs.NotFound{Kind: "link", Ref: linkID}
}

// SecureLinksForItem reads the links made for one item, URLs included: this is
// how one item is shared, which is what `items share get` answers.
func (s *Service) SecureLinksForItem(ctx context.Context, shareID, itemID string) ([]SecureLink, error) {
	links, err := s.secureLinks(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]SecureLink, 0)
	for _, l := range links {
		if l.ShareID != shareID || l.ItemID != itemID {
			continue
		}
		if url, err := s.rebuildLink(ctx, l.ShareID, l.ItemID, l.url, l.sealedKey, l.withItemKey); err == nil {
			l.URL = url
		}
		out = append(out, l)
	}
	return out, nil
}

// SecureLinksList reads every link this account has made.
//
// The URL is left out: it carries the key that opens the item, and a listing is
// not where a secret belongs. `links get` rebuilds one, and `items share get`
// the ones an item has.
func (s *Service) SecureLinksList(ctx context.Context) ([]SecureLink, error) {
	return s.secureLinks(ctx)
}

func (s *Service) secureLinks(ctx context.Context) ([]SecureLink, error) {
	var r struct {
		PublicLinks []struct {
			LinkID                      string
			ShareID                     string
			ItemID                      string
			LinkURL                     string
			Active                      bool
			ExpirationTime              int64
			ReadCount                   int
			MaxReadCount                int
			EncryptedLinkKey            string
			LinkKeyShareKeyRotation     int
			LinkKeyEncryptedWithItemKey bool
		}
	}
	if err := s.C.Decode(ctx, proton.Request{Method: "GET", Path: "/pass/v1/public_link"}, &r); err != nil {
		return nil, err
	}

	out := make([]SecureLink, 0, len(r.PublicLinks))
	for _, l := range r.PublicLinks {
		out = append(out, SecureLink{
			LinkID: l.LinkID, ShareID: l.ShareID, ItemID: l.ItemID,
			Active: l.Active, Expires: l.ExpirationTime,
			Reads: l.ReadCount, MaxReads: l.MaxReadCount,
			url: l.LinkURL, sealedKey: l.EncryptedLinkKey,
			withItemKey: l.LinkKeyEncryptedWithItemKey,
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Expires < out[j].Expires })
	return out, nil
}

// rebuildLink unseals a link key and puts the URL back together.
func (s *Service) rebuildLink(ctx context.Context, shareID, itemID, url, sealed string, withItemKey bool) (string, error) {
	if url == "" || sealed == "" {
		return "", fmt.Errorf("nothing to rebuild")
	}
	raw, err := base64.StdEncoding.DecodeString(sealed)
	if err != nil {
		return "", err
	}
	sk, err := s.decryptShareKeys(ctx, shareID)
	if err != nil {
		return "", err
	}
	// Older links were sealed under the share key rather than the item's.
	key, _ := sk.latest()
	if withItemKey {
		if key, _, err = s.latestItemKey(ctx, sk, shareID, itemID); err != nil {
			return "", err
		}
	}
	linkKey, err := aead.Decrypt(key, raw, []byte(aead.TagLinkKey))
	if err != nil {
		return "", err
	}
	return linkURL(url, linkKey), nil
}

// SecureLinkRevoke takes a link out of use. What it pointed at is untouched.
func (s *Service) SecureLinkRevoke(ctx context.Context, linkID string) error {
	return s.C.Decode(ctx, proton.Request{
		Method: "DELETE", Path: "/pass/v1/public_link/" + linkID,
	}, nil)
}
