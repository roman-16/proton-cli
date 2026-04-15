package cmd

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/roman-16/proton-cli/internal/client"
	"github.com/roman-16/proton-cli/internal/crypto"
	"github.com/spf13/cobra"
)

var driveCmd = &cobra.Command{
	Use:   "drive",
	Short: "Drive operations",
}

var driveRmPermanent bool

func init() {
	driveRmCmd.Flags().BoolVar(&driveRmPermanent, "permanent", false, "Permanently delete instead of trashing")
	driveTrashCmd.AddCommand(driveTrashListCmd, driveTrashRestoreCmd, driveTrashEmptyCmd)
	driveCmd.AddCommand(driveLsCmd, driveMkdirCmd, driveUploadCmd, driveDownloadCmd, driveRenameCmd, driveRmCmd, driveMvCmd, driveTrashCmd)
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

func getDefaultVolumeID(ctx context.Context, cl *client.Client) (string, error) {
	body, _, err := cl.Do(ctx, "GET", "/drive/volumes", nil, "", "", "")
	if err != nil {
		return "", err
	}
	var res struct {
		Volumes []struct{ VolumeID string }
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return "", err
	}
	if len(res.Volumes) == 0 {
		return "", fmt.Errorf("no volumes found")
	}
	return res.Volumes[0].VolumeID, nil
}
