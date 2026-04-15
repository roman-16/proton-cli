package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/roman-16/proton-cli/internal/crypto"
	"github.com/spf13/cobra"
)

var driveLsCmd = &cobra.Command{
	Use:   "ls [PATH]",
	Short: "List folder contents (decrypted names)",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runDriveLs,
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

	if flagJSON {
		out, _ := json.MarshalIndent(children, "", "  ")
		_, _ = os.Stdout.Write(out)
		fmt.Println()
		return nil
	}

	for _, child := range children {
		name, _ := child["DecryptedName"].(string)
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
