package cmd

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
	"github.com/roman-16/proton-cli/internal/client"
	"github.com/roman-16/proton-cli/internal/crypto"
	"github.com/spf13/cobra"
)

var driveCmd = &cobra.Command{
	Use:   "drive",
	Short: "Drive operations (encrypted)",
}

var driveLsCmd = &cobra.Command{
	Use:   "ls [PATH]",
	Short: "List folder contents (decrypted names)",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runDriveLs,
}

var driveMkdirCmd = &cobra.Command{
	Use:   "mkdir PATH",
	Short: "Create a folder",
	Args:  cobra.ExactArgs(1),
	RunE:  runDriveMkdir,
}

var driveUploadCmd = &cobra.Command{
	Use:   "upload LOCAL_FILE [REMOTE_PATH]",
	Short: "Upload a file",
	Args:  cobra.RangeArgs(1, 2),
	RunE:  runDriveUpload,
}

var driveDownloadCmd = &cobra.Command{
	Use:   "download REMOTE_PATH [LOCAL_PATH]",
	Short: "Download a file (decrypted)",
	Args:  cobra.RangeArgs(1, 2),
	RunE:  runDriveDownload,
}

var driveRenameCmd = &cobra.Command{
	Use:   "rename REMOTE_PATH NEW_NAME",
	Short: "Rename a file or folder",
	Args:  cobra.ExactArgs(2),
	RunE:  runDriveRename,
}

func init() {
	driveCmd.AddCommand(driveLsCmd, driveMkdirCmd, driveUploadCmd, driveDownloadCmd, driveRenameCmd)
	rootCmd.AddCommand(driveCmd)
}

// getDriveContext sets up auth, keys, and share for all drive commands.
func getDriveContext(ctx context.Context) (*client.Client, *crypto.KeyRings, *crypto.DriveKeys, error) {
	c, err := getAuthenticatedClient(ctx)
	if err != nil {
		return nil, nil, nil, err
	}

	password := getFlag(flagPassword, "PROTON_PASSWORD")
	kr, err := crypto.UnlockKeys(ctx, c, password)
	if err != nil {
		return nil, nil, nil, err
	}

	shareID, _, err := getDefaultShareAndLink(ctx, c)
	if err != nil {
		return nil, nil, nil, err
	}

	driveKeys, err := crypto.UnlockShare(ctx, c, shareID, kr)
	if err != nil {
		return nil, nil, nil, err
	}

	return c, kr, driveKeys, nil
}

func runDriveLs(cmd *cobra.Command, args []string) error {
	path := "/"
	if len(args) > 0 {
		path = args[0]
	}

	ctx := context.Background()
	c, kr, driveKeys, err := getDriveContext(ctx)
	if err != nil {
		return err
	}

	resolved, err := crypto.ResolvePath(ctx, c, driveKeys, path, kr)
	if err != nil {
		return err
	}

	if !resolved.IsFolder {
		return fmt.Errorf("%s is not a folder", path)
	}

	children, err := crypto.DecryptFolderChildren(ctx, c, resolved.ShareID, resolved.LinkID, resolved.NodeKR, driveKeys.AddrKR)
	if err != nil {
		return err
	}

	for _, child := range children {
		name := child["DecryptedName"]
		linkType, _ := child["Type"].(float64)
		size, _ := child["Size"].(float64)

		typeStr := "FILE"
		if int(linkType) == 1 {
			typeStr = "DIR "
		}

		fmt.Printf("%s  %10.0f  %s\n", typeStr, size, name)
	}

	return nil
}

func runDriveMkdir(cmd *cobra.Command, args []string) error {
	fullPath := args[0]
	parentPath := filepath.Dir(fullPath)
	folderName := filepath.Base(fullPath)

	ctx := context.Background()
	c, kr, driveKeys, err := getDriveContext(ctx)
	if err != nil {
		return err
	}

	parent, err := crypto.ResolvePath(ctx, c, driveKeys, parentPath, kr)
	if err != nil {
		return fmt.Errorf("parent folder not found: %w", err)
	}

	// Get parent hash key
	parentLink, err := crypto.GetLink(ctx, c, parent.ShareID, parent.LinkID)
	if err != nil {
		return err
	}
	hashKey, err := crypto.GetHashKey(parentLink, parent.NodeKR)
	if err != nil {
		return fmt.Errorf("failed to get hash key: %w", err)
	}

	hash, err := crypto.GenerateLookupHash(strings.ToLower(folderName), hashKey)
	if err != nil {
		return err
	}

	encName, err := crypto.EncryptName(folderName, parent.NodeKR, driveKeys.AddrKR)
	if err != nil {
		return err
	}

	nodeKey, nodePassphrase, nodePassphraseSig, nodePrivKey, err := crypto.GenerateNodeKeys(parent.NodeKR, driveKeys.AddrKR)
	if err != nil {
		return err
	}

	nodeKR, err := pgp.NewKeyRing(nodePrivKey)
	if err != nil {
		return err
	}
	nodeHashKey, err := crypto.GenerateNodeHashKey(nodeKR, nodeKR)
	if err != nil {
		return err
	}

	reqBody := map[string]interface{}{
		"Name":                    encName,
		"Hash":                    hash,
		"ParentLinkID":            parent.LinkID,
		"NodePassphrase":          nodePassphrase,
		"NodePassphraseSignature": nodePassphraseSig,
		"SignatureAddress":        driveKeys.AddrEmail,
		"NodeKey":                 nodeKey,
		"NodeHashKey":             nodeHashKey,
	}

	body, _ := json.Marshal(reqBody)
	resp, statusCode, err := c.Do(ctx, "POST", "/drive/shares/"+parent.ShareID+"/folders", nil, string(body), "", "")
	if err != nil {
		return err
	}

	if statusCode >= 400 {
		fmt.Fprintf(os.Stderr, "Error: %s\n", string(resp))
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "Folder created: %s\n", fullPath)
	return nil
}

func runDriveUpload(cmd *cobra.Command, args []string) error {
	localPath := args[0]
	remotePath := "/"
	if len(args) >= 2 {
		remotePath = args[1]
	}

	ctx := context.Background()
	c, kr, driveKeys, err := getDriveContext(ctx)
	if err != nil {
		return err
	}

	parent, err := crypto.ResolvePath(ctx, c, driveKeys, remotePath, kr)
	if err != nil {
		return fmt.Errorf("target folder not found: %w", err)
	}

	parentLink, err := crypto.GetLink(ctx, c, parent.ShareID, parent.LinkID)
	if err != nil {
		return err
	}
	hashKey, err := crypto.GetHashKey(parentLink, parent.NodeKR)
	if err != nil {
		return fmt.Errorf("failed to get hash key: %w", err)
	}

	fileName := filepath.Base(localPath)
	hash, err := crypto.GenerateLookupHash(strings.ToLower(fileName), hashKey)
	if err != nil {
		return err
	}

	encName, err := crypto.EncryptName(fileName, parent.NodeKR, driveKeys.AddrKR)
	if err != nil {
		return err
	}

	nodeKey, nodePassphrase, nodePassphraseSig, nodePrivKey, err := crypto.GenerateNodeKeys(parent.NodeKR, driveKeys.AddrKR)
	if err != nil {
		return err
	}

	nodeKR, err := pgp.NewKeyRing(nodePrivKey)
	if err != nil {
		return err
	}

	sessionKey, contentKeyPacket, contentKeyPacketSig, err := crypto.GenerateFileKeys(nodeKR, driveKeys.AddrKR)
	if err != nil {
		return err
	}

	// Step 1: Create file
	createReq := map[string]interface{}{
		"Name":                      encName,
		"Hash":                      hash,
		"ParentLinkID":              parent.LinkID,
		"NodePassphrase":            nodePassphrase,
		"NodePassphraseSignature":   nodePassphraseSig,
		"SignatureAddress":          driveKeys.AddrEmail,
		"NodeKey":                   nodeKey,
		"MIMEType":                  "application/octet-stream",
		"ContentKeyPacket":          contentKeyPacket,
		"ContentKeyPacketSignature": contentKeyPacketSig,
	}

	createBody, _ := json.Marshal(createReq)
	createResp, _, err := c.Do(ctx, "POST", "/drive/shares/"+parent.ShareID+"/files", nil, string(createBody), "", "")
	if err != nil {
		return err
	}

	var createResult struct {
		Code int
		File struct {
			ID         string
			RevisionID string
		}
	}
	if err := json.Unmarshal(createResp, &createResult); err != nil {
		return err
	}
	if createResult.Code != 1000 {
		return fmt.Errorf("create file failed: %s", string(createResp))
	}

	linkID := createResult.File.ID
	revisionID := createResult.File.RevisionID

	fmt.Fprintf(os.Stderr, "Uploading %s...\n", fileName)

	// Step 2: Get verification data
	verResp, _, err := c.Do(ctx, "GET",
		fmt.Sprintf("/drive/shares/%s/links/%s/revisions/%s/verification", parent.ShareID, linkID, revisionID),
		nil, "", "", "")
	if err != nil {
		return fmt.Errorf("failed to get verification data: %w", err)
	}

	var verResult struct {
		VerificationCode string
		ContentKeyPacket string
	}
	if err := json.Unmarshal(verResp, &verResult); err != nil {
		return err
	}

	verCode, err := base64.StdEncoding.DecodeString(verResult.VerificationCode)
	if err != nil {
		return fmt.Errorf("failed to decode verification code: %w", err)
	}

	// Step 3: Read file and encrypt blocks
	fileData, err := os.ReadFile(localPath)
	if err != nil {
		return err
	}

	const blockSize = 4 * 1024 * 1024
	type blockInfo struct {
		Index         int
		Hash          string
		EncSig        string
		Size          int
		EncData       []byte
		VerifierToken string
	}
	var blocks []blockInfo

	for i := 0; i*blockSize < len(fileData); i++ {
		start := i * blockSize
		end := start + blockSize
		if end > len(fileData) {
			end = len(fileData)
		}
		chunk := fileData[start:end]

		encData, encSig, err := crypto.EncryptBlock(chunk, sessionKey, nodeKR, driveKeys.AddrKR)
		if err != nil {
			return fmt.Errorf("failed to encrypt block %d: %w", i+1, err)
		}

		h := sha256.Sum256(encData)

		verToken := make([]byte, len(verCode))
		for j := 0; j < len(verCode); j++ {
			if j < len(encData) {
				verToken[j] = verCode[j] ^ encData[j]
			} else {
				verToken[j] = verCode[j]
			}
		}

		blocks = append(blocks, blockInfo{
			Index:         i + 1,
			Hash:          base64.StdEncoding.EncodeToString(h[:]),
			EncSig:        encSig,
			Size:          len(encData),
			EncData:       encData,
			VerifierToken: base64.StdEncoding.EncodeToString(verToken),
		})
	}

	// Step 4: Request upload URLs
	blockList := make([]map[string]interface{}, len(blocks))
	for i, b := range blocks {
		blockList[i] = map[string]interface{}{
			"Hash":         b.Hash,
			"EncSignature": b.EncSig,
			"Size":         b.Size,
			"Index":        b.Index,
			"Verifier":     map[string]string{"Token": b.VerifierToken},
		}
	}

	uploadReq := map[string]interface{}{
		"AddressID":  driveKeys.AddrID,
		"ShareID":    parent.ShareID,
		"LinkID":     linkID,
		"RevisionID": revisionID,
		"BlockList":  blockList,
	}

	uploadBody, _ := json.Marshal(uploadReq)
	uploadResp, _, err := c.Do(ctx, "POST", "/drive/blocks", nil, string(uploadBody), "", "")
	if err != nil {
		return err
	}

	var uploadResult struct {
		Code        int
		UploadLinks []struct {
			Token   string
			BareURL string
		}
	}
	if err := json.Unmarshal(uploadResp, &uploadResult); err != nil {
		return err
	}
	if uploadResult.Code != 1000 {
		return fmt.Errorf("request block upload failed: %s", string(uploadResp))
	}

	// Step 5: Upload each block
	for i, link := range uploadResult.UploadLinks {
		fmt.Fprintf(os.Stderr, "  block %d/%d\n", i+1, len(blocks))

		var buf strings.Builder
		boundary := "proton-cli-boundary"
		buf.WriteString("--" + boundary + "\r\n")
		buf.WriteString("Content-Disposition: form-data; name=\"Block\"; filename=\"blob\"\r\n")
		buf.WriteString("Content-Type: application/octet-stream\r\n\r\n")
		buf.Write(blocks[i].EncData)
		buf.WriteString("\r\n--" + boundary + "--\r\n")

		req, err := http.NewRequest("POST", link.BareURL, strings.NewReader(buf.String()))
		if err != nil {
			return err
		}
		req.Header.Set("pm-storage-token", link.Token)
		req.Header.Set("Content-Type", "multipart/form-data; boundary="+boundary)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return fmt.Errorf("upload block %d failed: %w", i+1, err)
		}
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode >= 400 {
			return fmt.Errorf("upload block %d returned %d: %s", i+1, resp.StatusCode, string(respBody))
		}
	}

	// Step 6: Finalize revision
	var manifestBytes []byte
	for _, b := range blocks {
		hashBytes, _ := base64.StdEncoding.DecodeString(b.Hash)
		manifestBytes = append(manifestBytes, hashBytes...)
	}
	manifestMsg := pgp.NewPlainMessage(manifestBytes)
	manifestSigObj, err := driveKeys.AddrKR.SignDetached(manifestMsg)
	if err != nil {
		return err
	}
	manifestSig, err := manifestSigObj.GetArmored()
	if err != nil {
		return err
	}

	blockTokens := make([]map[string]interface{}, len(uploadResult.UploadLinks))
	for i, link := range uploadResult.UploadLinks {
		blockTokens[i] = map[string]interface{}{
			"Index": blocks[i].Index,
			"Token": link.Token,
		}
	}

	finalizeReq := map[string]interface{}{
		"BlockList":         blockTokens,
		"State":             1,
		"ManifestSignature": manifestSig,
		"SignatureAddress":  driveKeys.AddrEmail,
	}

	finalizeBody, _ := json.Marshal(finalizeReq)
	finalizeResp, statusCode, err := c.Do(ctx, "PUT",
		fmt.Sprintf("/drive/shares/%s/files/%s/revisions/%s", parent.ShareID, linkID, revisionID),
		nil, string(finalizeBody), "", "")
	if err != nil {
		return err
	}

	if statusCode >= 400 {
		return fmt.Errorf("finalize failed: %s", string(finalizeResp))
	}

	fmt.Fprintf(os.Stderr, "Uploaded %s\n", fileName)
	return nil
}

func runDriveDownload(cmd *cobra.Command, args []string) error {
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

	if resolved.IsFolder {
		return fmt.Errorf("%s is a folder, not a file", remotePath)
	}

	// Get file link for content key packet
	fileLink, err := crypto.GetLink(ctx, c, resolved.ShareID, resolved.LinkID)
	if err != nil {
		return err
	}

	var fileProps struct {
		ContentKeyPacket string
		ActiveRevision   struct {
			ID string
		}
	}
	linkBody, _, err := c.Do(ctx, "GET", fmt.Sprintf("/drive/shares/%s/links/%s", resolved.ShareID, resolved.LinkID), nil, "", "", "")
	if err != nil {
		return err
	}
	var linkResp struct {
		Link struct {
			FileProperties struct {
				ContentKeyPacket string
				ActiveRevision   struct {
					ID string
				}
			}
		}
	}
	json.Unmarshal(linkBody, &linkResp)
	fileProps = linkResp.Link.FileProperties
	_ = fileLink

	sessionKey, err := crypto.GetFileSessionKey(fileProps.ContentKeyPacket, resolved.NodeKR)
	if err != nil {
		return fmt.Errorf("failed to get file session key: %w", err)
	}

	// Get revision blocks
	revResp, _, err := c.Do(ctx, "GET",
		fmt.Sprintf("/drive/shares/%s/files/%s/revisions/%s", resolved.ShareID, resolved.LinkID, fileProps.ActiveRevision.ID),
		map[string]string{"FromBlockIndex": "1", "PageSize": "50"}, "", "", "")
	if err != nil {
		return err
	}

	var revResult struct {
		Revision struct {
			Blocks []struct {
				Index   int
				BareURL string
				Token   string
			}
		}
	}
	if err := json.Unmarshal(revResp, &revResult); err != nil {
		return err
	}

	var fileData []byte
	for i, block := range revResult.Revision.Blocks {
		fmt.Fprintf(os.Stderr, "Downloading block %d/%d...\n", i+1, len(revResult.Revision.Blocks))

		req, err := http.NewRequest("GET", block.BareURL, nil)
		if err != nil {
			return err
		}
		req.Header.Set("pm-storage-token", block.Token)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return fmt.Errorf("download block %d failed: %w", i+1, err)
		}
		encData, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return err
		}

		decData, err := crypto.DecryptBlock(encData, sessionKey)
		if err != nil {
			return fmt.Errorf("decrypt block %d failed: %w", i+1, err)
		}
		fileData = append(fileData, decData...)
	}

	// Write to file or stdout
	if len(args) >= 2 {
		outputPath := args[1]
		if err := os.WriteFile(outputPath, fileData, 0644); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "Downloaded to %s (%d bytes)\n", outputPath, len(fileData))
	} else {
		os.Stdout.Write(fileData)
	}

	return nil
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

	// Get parent's hash key
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
		"Name":               encName,
		"Hash":               newHash,
		"OriginalHash":       oldHash,
		"NameSignatureEmail": driveKeys.AddrEmail,
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

// ── Helpers ──

func getDefaultShareAndLink(ctx context.Context, cl *client.Client) (string, string, error) {
	body, _, err := cl.Do(ctx, "GET", "/drive/volumes", nil, "", "", "")
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
