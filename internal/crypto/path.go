package crypto

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
	"github.com/roman-16/proton-cli/internal/client"
)

// ResolvedPath holds the result of resolving a Drive path.
type ResolvedPath struct {
	ShareID  string
	LinkID   string
	ParentKR *pgp.KeyRing
	NodeKR   *pgp.KeyRing
	Name     string
	IsFolder bool
}

// ResolvePath walks a path like "/Documents/Photos/pic.jpg" from the root
// and returns the share ID, link ID, and key rings for the final node.
func ResolvePath(ctx context.Context, c *client.Client, driveKeys *DriveKeys, path string, kr *KeyRings) (*ResolvedPath, error) {
	// Get default share and root link
	shareID, rootLinkID, err := getDefaultVolume(ctx, c)
	if err != nil {
		return nil, err
	}

	// Clean the path
	path = strings.Trim(path, "/")
	if path == "" || path == "." {
		// Root
		rootLink, err := GetLink(ctx, c, shareID, rootLinkID)
		if err != nil {
			return nil, err
		}
		rootKR, err := UnlockNode(rootLink, driveKeys.ShareKR, driveKeys.AddrKR)
		if err != nil {
			return nil, err
		}
		return &ResolvedPath{
			ShareID:  shareID,
			LinkID:   rootLinkID,
			ParentKR: driveKeys.ShareKR,
			NodeKR:   rootKR,
			Name:     "",
			IsFolder: true,
		}, nil
	}

	parts := strings.Split(path, "/")

	// Start at root
	currentLinkID := rootLinkID
	rootLink, err := GetLink(ctx, c, shareID, rootLinkID)
	if err != nil {
		return nil, err
	}
	parentKR, err := UnlockNode(rootLink, driveKeys.ShareKR, driveKeys.AddrKR)
	if err != nil {
		return nil, fmt.Errorf("failed to unlock root: %w", err)
	}
	currentKR := parentKR

	// Walk each path component
	for i, part := range parts {
		isLast := i == len(parts)-1

		// List children of current folder
		children, err := listRawChildren(ctx, c, shareID, currentLinkID)
		if err != nil {
			return nil, fmt.Errorf("failed to list children of %s: %w", currentLinkID, err)
		}

		// Find matching child by decrypted name
		found := false
		for _, child := range children {
			name, err := DecryptName(child.Name, currentKR)
			if err != nil {
				continue
			}

			if name == part {
				found = true
				prevKR := currentKR

				childKR, err := UnlockNode(&child, currentKR, driveKeys.AddrKR)
				if err != nil {
					return nil, fmt.Errorf("failed to unlock %s: %w", name, err)
				}

				if isLast {
					return &ResolvedPath{
						ShareID:  shareID,
						LinkID:   child.LinkID,
						ParentKR: prevKR,
						NodeKR:   childKR,
						Name:     name,
						IsFolder: child.Type == 1,
					}, nil
				}

				if child.Type != 1 {
					return nil, fmt.Errorf("%s is not a folder", name)
				}

				currentLinkID = child.LinkID
				parentKR = currentKR
				currentKR = childKR
				break
			}
		}

		if !found {
			return nil, fmt.Errorf("not found: %s", part)
		}
	}

	return nil, fmt.Errorf("path resolution failed")
}

func getDefaultVolume(ctx context.Context, c *client.Client) (string, string, error) {
	body, _, err := c.Do(ctx, "GET", "/drive/volumes", nil, "", "", "")
	if err != nil {
		return "", "", err
	}
	var res struct {
		Volumes []struct {
			Share struct {
				ShareID string
				LinkID  string
			}
		}
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return "", "", err
	}
	if len(res.Volumes) == 0 {
		return "", "", fmt.Errorf("no volumes found")
	}
	return res.Volumes[0].Share.ShareID, res.Volumes[0].Share.LinkID, nil
}

func listRawChildren(ctx context.Context, c *client.Client, shareID, linkID string) ([]link, error) {
	var allLinks []link
	page := 0
	for {
		body, _, err := c.Do(ctx, "GET",
			fmt.Sprintf("/drive/shares/%s/folders/%s/children", shareID, linkID),
			map[string]string{"Page": fmt.Sprintf("%d", page), "PageSize": "150"}, "", "", "")
		if err != nil {
			return nil, err
		}
		var res struct {
			Links []link
		}
		if err := json.Unmarshal(body, &res); err != nil {
			return nil, err
		}
		if len(res.Links) == 0 {
			break
		}
		allLinks = append(allLinks, res.Links...)
		if len(res.Links) < 150 {
			break
		}
		page++
	}
	return allLinks, nil
}
