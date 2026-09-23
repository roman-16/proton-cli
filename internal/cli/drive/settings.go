package drive

import (
	"encoding/json"

	"github.com/roman-16/proton-cli/internal/cli/kit"
	"github.com/roman-16/proton-cli/internal/proton"
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/spf13/cobra"
)

const settingsPath = "/drive/me/settings"

// Drive has one settings page, Version history.
var specs = map[string]kit.Setting{
	"version-history": {
		Path: settingsPath, Field: "RevisionRetentionDays",
		Page: "Version history", Desc: "How long earlier versions of a file are kept",
		Enum: []kit.Choice{
			{Name: "off", Value: 0}, {Name: "7d", Value: 7}, {Name: "30d", Value: 30},
			{Name: "180d", Value: 180}, {Name: "1y", Value: 365}, {Name: "10y", Value: 3650},
		},
	},
}

type settingsView struct {
	VersionHistory string `json:"version_history"`
}

func settingsCmd() *cobra.Command {
	return kit.Settings("drive", "How Drive behaves", specs, settingsView{}, func(c *kit.Invocation) error {
		resp, err := c.App.API.Do(c.Ctx, proton.Request{Method: "GET", Path: settingsPath})
		if err != nil {
			return err
		}
		var env struct {
			UserSettings struct {
				RevisionRetentionDays any
			}
		}
		if err := json.Unmarshal(resp.Body, &env); err != nil {
			return err
		}
		view := settingsView{
			VersionHistory: specs["version-history"].Name(env.UserSettings.RevisionRetentionDays),
		}
		return kit.Show(c, ui.RecordSpec{
			Object: view,
			Fields: []ui.Field{{Label: "Version History", Value: view.VersionHistory, Always: true}},
		})
	})
}
