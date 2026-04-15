package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/roman-16/proton-cli/internal/crypto"
	"github.com/spf13/cobra"
)

var driveRenameCmd = &cobra.Command{
	Use:   "rename REMOTE_PATH NEW_NAME",
	Short: "Rename a file or folder",
	Args:  cobra.ExactArgs(2),
	RunE:  runDriveRename,
}

var driveRmCmd = &cobra.Command{
	Use:   "rm REMOTE_PATH",
	Short: "Delete a file or folder (move to trash)",
	Args:  cobra.ExactArgs(1),
	RunE:  runDriveRm,
}

var driveMvCmd = &cobra.Command{
	Use:   "mv SOURCE_PATH DEST_FOLDER",
	Short: "Move a file or folder to a different folder",
	Args:  cobra.ExactArgs(2),
	RunE:  runDriveMv,
}

func runDriveRename(cmd *cobra.Command, args []string) error {
	remotePath := args[0]
	newName := args[1]

	ctx := context.Background()
	c, kr, driveKeys, err := getDriveContext(ctx)
	if err != nil {
		return err
	}

	resolved, err := crypto.ResolvePath(ctx, c, driveKeys, remotePath, kr)
	if err != nil {
		return err
	}

	parentLink, err := crypto.GetLink(ctx, c, resolved.ShareID,
		func() string {
			l, _ := crypto.GetLink(ctx, c, resolved.ShareID, resolved.LinkID)
			return l.ParentLinkID
		}())
	if err != nil {
		return err
	}

	hashKey, err := crypto.GetHashKey(parentLink, resolved.ParentKR)
	if err != nil {
		return fmt.Errorf("failed to get hash key: %w", err)
	}

	newHash, err := crypto.GenerateLookupHash(strings.ToLower(newName), hashKey)
	if err != nil {
		return err
	}
	oldHash, _ := crypto.GenerateLookupHash(strings.ToLower(resolved.Name), hashKey)
	encName, err := crypto.EncryptName(newName, resolved.ParentKR, driveKeys.AddrKR)
	if err != nil {
		return err
	}

	reqBody := map[string]interface{}{
		"Name": encName, "Hash": newHash,
		"OriginalHash": oldHash, "NameSignatureEmail": driveKeys.AddrEmail,
	}

	body, _ := json.Marshal(reqBody)
	resp, statusCode, err := c.Do(ctx, "PUT",
		fmt.Sprintf("/drive/shares/%s/links/%s/rename", resolved.ShareID, resolved.LinkID),
		nil, string(body), "", "")
	if err != nil {
		return err
	}
	if statusCode >= 400 {
		return fmt.Errorf("rename failed: %s", string(resp))
	}

	fmt.Fprintf(os.Stderr, "Renamed to %s\n", newName)
	return nil
}

func runDriveRm(cmd *cobra.Command, args []string) error {
	remotePath := args[0]

	ctx := context.Background()
	c, kr, driveKeys, err := getDriveContext(ctx)
	if err != nil {
		return err
	}

	resolved, err := crypto.ResolvePath(ctx, c, driveKeys, remotePath, kr)
	if err != nil {
		return err
	}

	if driveRmPermanent {
		// WebClients flow: trash first, then delete from trash
		volumeID, err := getDefaultVolumeID(ctx, c)
		if err != nil {
			return err
		}

		// Step 1: Trash
		trashBody, _ := json.Marshal(map[string]interface{}{"LinkIDs": []string{resolved.LinkID}})
		resp, statusCode, err := c.Do(ctx, "POST",
			"/drive/v2/volumes/"+volumeID+"/trash_multiple",
			nil, string(trashBody), "", "")
		if err != nil {
			return err
		}
		if statusCode >= 400 {
			return fmt.Errorf("trash failed: %s", string(resp))
		}

		// Step 2: Permanently delete from trash
		delBody, _ := json.Marshal(map[string]interface{}{"LinkIDs": []string{resolved.LinkID}})
		resp, statusCode, err = c.Do(ctx, "POST",
			"/drive/v2/volumes/"+volumeID+"/trash/delete_multiple",
			nil, string(delBody), "", "")
		if err != nil {
			return err
		}
		if statusCode >= 400 {
			return fmt.Errorf("permanent delete failed: %s", string(resp))
		}
		fmt.Fprintf(os.Stderr, "Permanently deleted: %s\n", remotePath)
	} else {
		volumeID, err := getDefaultVolumeID(ctx, c)
		if err != nil {
			return err
		}
		reqBody := map[string]interface{}{"LinkIDs": []string{resolved.LinkID}}
		body, _ := json.Marshal(reqBody)
		resp, statusCode, err := c.Do(ctx, "POST",
			"/drive/v2/volumes/"+volumeID+"/trash_multiple",
			nil, string(body), "", "")
		if err != nil {
			return err
		}
		if statusCode >= 400 {
			return fmt.Errorf("trash failed: %s", string(resp))
		}
		fmt.Fprintf(os.Stderr, "Moved to trash: %s\n", remotePath)
	}

	return nil
}

func runDriveMv(cmd *cobra.Command, args []string) error {
	sourcePath := args[0]
	destPath := args[1]

	ctx := context.Background()
	c, kr, driveKeys, err := getDriveContext(ctx)
	if err != nil {
		return err
	}

	source, err := crypto.ResolvePath(ctx, c, driveKeys, sourcePath, kr)
	if err != nil {
		return fmt.Errorf("source not found: %w", err)
	}

	dest, err := crypto.ResolvePath(ctx, c, driveKeys, destPath, kr)
	if err != nil {
		return fmt.Errorf("destination not found: %w", err)
	}
	if !dest.IsFolder {
		return fmt.Errorf("%s is not a folder", destPath)
	}

	sourceLink, err := crypto.GetLink(ctx, c, source.ShareID, source.LinkID)
	if err != nil {
		return err
	}

	destLink, err := crypto.GetLink(ctx, c, dest.ShareID, dest.LinkID)
	if err != nil {
		return err
	}
	hashKey, err := crypto.GetHashKey(destLink, dest.NodeKR)
	if err != nil {
		return fmt.Errorf("failed to get destination hash key: %w", err)
	}

	newHash, err := crypto.GenerateLookupHash(strings.ToLower(source.Name), hashKey)
	if err != nil {
		return err
	}

	// Re-encrypt name preserving session key (matches WebClients move flow)
	encName, err := crypto.ReEncryptName(sourceLink.Name, source.Name, source.ParentKR, dest.NodeKR, driveKeys.AddrKR)
	if err != nil {
		return err
	}

	nodePassphrase, _, err := crypto.ReEncryptNodePassphrase(sourceLink, source.ParentKR, dest.NodeKR, driveKeys.AddrKR)
	if err != nil {
		return fmt.Errorf("failed to re-encrypt passphrase: %w", err)
	}

	reqBody := map[string]interface{}{
		"Name":               encName,
		"Hash":               newHash,
		"ParentLinkID":       dest.LinkID,
		"NodePassphrase":     nodePassphrase,
		"NameSignatureEmail": driveKeys.AddrEmail,
	}

	body, _ := json.Marshal(reqBody)
	resp, statusCode, err := c.Do(ctx, "PUT",
		fmt.Sprintf("/drive/shares/%s/links/%s/move", source.ShareID, source.LinkID),
		nil, string(body), "", "")
	if err != nil {
		return err
	}
	if statusCode >= 400 {
		return fmt.Errorf("move failed: %s", string(resp))
	}

	fmt.Fprintf(os.Stderr, "Moved %s → %s\n", sourcePath, destPath)
	return nil
}
