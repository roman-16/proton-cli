package mail

import (
	"encoding/json"

	"github.com/roman-16/proton-cli/internal/cli/kit"
	"github.com/roman-16/proton-cli/internal/proton"
	mailsvc "github.com/roman-16/proton-cli/internal/service/mail"
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/spf13/cobra"
)

const settingsPath = "/mail/v4/settings"

// specs mirrors the writable scalars on Proton's Mail "General" and "Email
// privacy" pages.
//
// Settings the API stores through a shaped request rather than a plain value -
// the image proxy, the spam action - are absent, and the structured pages get
// their own subcommands instead.
var specs = map[string]kit.Setting{
	"almost-all-mail": {
		Path: settingsPath + "/almost-all-mail", Field: "AlmostAllMail",
		Page: "General", Desc: "Exclude spam and trash from All mail", Enum: kit.OnOffChoices(),
	},
	"attach-public-key": {
		Path: settingsPath + "/attachpublic", Field: "AttachPublicKey",
		Page: "General", Desc: "Attach your public key to outgoing mail", Enum: kit.OnOffChoices(),
	},
	"auto-delete-spam-trash": {
		Path: settingsPath + "/auto-delete-spam-and-trash-days", Field: "Days",
		Page: "General", Desc: "Delete spam and trash permanently after this long",
		Enum: []kit.Choice{{Name: "off", N: 0}, {Name: "30d", N: 30}},
	},
	"auto-save-contacts": {
		Path: settingsPath + "/autocontacts", Field: "AutoSaveContacts",
		Page: "General", Desc: "Add unknown recipients to Contacts", Enum: kit.OnOffChoices(),
	},
	"composer-mode": {
		Path: settingsPath + "/composermode", Field: "ComposerMode",
		Page: "General", Desc: "How the web composer opens",
		Enum: kit.Ordered("popup", "maximized"),
	},
	"confirm-link": {
		Path: settingsPath + "/confirmlink", Field: "ConfirmLink",
		Page: "General", Desc: "Confirm before opening an external link", Enum: kit.OnOffChoices(),
	},
	"delay-send": {
		Path: settingsPath + "/delaysend", Field: "DelaySendSeconds",
		Page: "General", Desc: "How long the undo-send window lasts",
		Range: &kit.IntRange{Min: 0, Max: 20, Unit: "seconds"},
	},
	"draft-type": {
		Path: settingsPath + "/drafttype", Field: "MIMEType",
		Page: "General", Desc: "Default composer format", Text: []string{"text/html", "text/plain"},
	},
	"enable-folder-color": {
		Path: settingsPath + "/enablefoldercolor", Field: "EnableFolderColor",
		Page: "General", Desc: "Colour folders in the sidebar", Enum: kit.OnOffChoices(),
	},
	"hide-embedded-images": {
		Path: settingsPath + "/hide-embedded-images", Field: "HideEmbeddedImages",
		Page: "Email privacy", Desc: "Block images embedded in messages", Enum: kit.OnOffChoices(),
	},
	"hide-remote-images": {
		Path: settingsPath + "/hide-remote-images", Field: "HideRemoteImages",
		Page: "Email privacy", Desc: "Block images loaded from the internet", Enum: kit.OnOffChoices(),
	},
	"hide-sender-images": {
		Path: settingsPath + "/hide-sender-images", Field: "HideSenderImages",
		Page: "Email privacy", Desc: "Block sender profile pictures", Enum: kit.OnOffChoices(),
	},
	"inherit-folder-color": {
		Path: settingsPath + "/inheritparentfoldercolor", Field: "InheritParentFolderColor",
		Page: "General", Desc: "Subfolders inherit their parent's colour", Enum: kit.OnOffChoices(),
	},
	"message-buttons": {
		Path: settingsPath + "/messagebuttons", Field: "MessageButtons",
		Page: "General", Desc: "Order of the read and unread buttons",
		Enum: kit.Ordered("read-unread", "unread-read"),
	},
	"page-size": {
		Path: settingsPath + "/pagesize", Field: "PageSize",
		Page: "General", Desc: "Messages per page in the web client",
		Enum: []kit.Choice{{Name: "50", N: 50}, {Name: "100", N: 100}, {Name: "200", N: 200}},
	},
	"pm-signature": {
		Path: settingsPath + "/pmsignature", Field: "PMSignature",
		Page: "General", Desc: `Append "Sent with Proton Mail secure email."`, Enum: kit.OnOffChoices(),
	},
	"prompt-pin": {
		Path: settingsPath + "/promptpin", Field: "PromptPin",
		Page: "General", Desc: "Offer to pin the keys of contacts who sign their mail",
		Enum: kit.OnOffChoices(),
	},
	"shortcuts": {
		Path: settingsPath + "/shortcuts", Field: "Shortcuts",
		Page: "General", Desc: "Keyboard shortcuts in the web client", Enum: kit.OnOffChoices(),
	},
	"show-moved": {
		Path: settingsPath + "/moved", Field: "ShowMoved",
		Page: "General", Desc: "Keep moved drafts and sent mail in their folders",
		Enum: kit.Ordered("none", "drafts", "sent", "drafts-and-sent"),
	},
	"sign": {
		Path: settingsPath + "/sign", Field: "Sign",
		Page: "General", Desc: "Sign outgoing mail by default", Enum: kit.OnOffChoices(),
	},
	"sticky-labels": {
		Path: settingsPath + "/stickylabels", Field: "StickyLabels",
		Page: "General", Desc: "Keep a label when moving a message", Enum: kit.OnOffChoices(),
	},
	"unread-favicon": {
		Path: settingsPath + "/unread-favicon", Field: "UnreadFavicon",
		Page: "General", Desc: "Show the unread count in the browser tab", Enum: kit.OnOffChoices(),
	},
	"view-layout": {
		Path: settingsPath + "/viewlayout", Field: "ViewLayout",
		Page: "General", Desc: "Mailbox layout", Enum: kit.Ordered("column", "row"),
	},
	"next-message-on-move": {
		Path: settingsPath + "/next-message-on-move", Field: "NextMessageOnMove",
		Page: "General", Desc: "Open the next message after moving one", Enum: kit.OnOffChoices(),
	},
	"pgp-scheme": {
		Path: settingsPath + "/pgpscheme", Field: "PGPScheme",
		Page: "Encryption and keys", Desc: "How mail to external PGP recipients is packaged",
		// Proton stores these as the package-type bits they select.
		Enum: []kit.Choice{{Name: "pgp-mime", N: 16}, {Name: "pgp-inline", N: 8}},
	},
	"remove-image-metadata": {
		Path: settingsPath + "/remove-image-metadata", Field: "RemoveImageMetadata",
		Page: "Email privacy", Desc: "Strip EXIF and location from images you attach",
		Enum: kit.OnOffChoices(),
	},
	"right-to-left": {
		Path: settingsPath + "/righttoleft", Field: "RightToLeft",
		Page: "General", Desc: "Compose right to left",
		Enum: kit.Ordered("left-to-right", "right-to-left"),
	},
	"spam-action": {
		Path: settingsPath + "/spam-action", Field: "SpamAction",
		Page: "General", Desc: "What moving to spam also does",
		Enum: []kit.Choice{{Name: "just-move", N: 0}, {Name: "move-and-unsubscribe", N: 1}},
	},
	"view-mode": {
		Path: settingsPath + "/viewmode", Field: "ViewMode",
		Page: "General", Desc: "Group mail into threads or list single messages",
		Enum: kit.Ordered("conversations", "messages"),
	},
}

// settingsView is the shape `mail settings get` reports: declared, snake_case, and
// speaking the same value names `set` accepts.
type settingsView struct {
	DisplayName    string `json:"display_name,omitempty"`
	PageSize       string `json:"page_size"`
	ViewMode       string `json:"view_mode"`
	ViewLayout     string `json:"view_layout"`
	DraftType      string `json:"draft_type"`
	ShowMoved      string `json:"show_moved"`
	ProtonFooter   string `json:"pm_signature"`
	Sign           string `json:"sign"`
	AttachKey      string `json:"attach_public_key"`
	SaveContacts   string `json:"auto_save_contacts"`
	RemoteImages   string `json:"hide_remote_images"`
	EmbeddedImages string `json:"hide_embedded_images"`
	Shortcuts      string `json:"shortcuts"`
	DelaySend      string `json:"delay_send"`
	AutoReply      string `json:"auto_reply"`
}

func settingsCmd() *cobra.Command {
	c := kit.Settings("mail", "How Mail behaves", specs, func(c *kit.Invocation) error {
		resp, err := c.App.API.Do(c.Ctx, proton.Request{Method: "GET", Path: settingsPath})
		if err != nil {
			return err
		}
		var env struct {
			MailSettings struct {
				DisplayName        string
				DraftMIMEType      string
				PageSize           any
				ViewMode           any
				ViewLayout         any
				ShowMoved          any
				PMSignature        any
				Sign               any
				AttachPublicKey    any
				AutoSaveContacts   any
				HideRemoteImages   any
				HideEmbeddedImages any
				Shortcuts          any
				DelaySendSeconds   any
				AutoResponder      map[string]any
			}
		}
		if err := json.Unmarshal(resp.Body, &env); err != nil {
			return err
		}
		m := env.MailSettings

		view := settingsView{
			DisplayName: m.DisplayName,
			PageSize:    specs["page-size"].Name(m.PageSize),
			ViewMode:    specs["view-mode"].Name(m.ViewMode),
			ViewLayout:  specs["view-layout"].Name(m.ViewLayout),
			DraftType:   m.DraftMIMEType,
			ShowMoved:   specs["show-moved"].Name(m.ShowMoved),
			// PMSignature carries more than one bit; only the low one says
			// whether the footer is appended.
			ProtonFooter:   kit.OnOffText(kit.IntOf(m.PMSignature) & 1),
			Sign:           kit.OnOffText(kit.IntOf(m.Sign)),
			AttachKey:      kit.OnOffText(kit.IntOf(m.AttachPublicKey)),
			SaveContacts:   kit.OnOffText(kit.IntOf(m.AutoSaveContacts)),
			RemoteImages:   kit.OnOffText(kit.IntOf(m.HideRemoteImages)),
			EmbeddedImages: kit.OnOffText(kit.IntOf(m.HideEmbeddedImages)),
			Shortcuts:      kit.OnOffText(kit.IntOf(m.Shortcuts)),
			DelaySend:      kit.OnOffText(0),
			AutoReply:      autoReplySummary(m.AutoResponder),
		}
		view.DelaySend = secondsText(kit.IntOf(m.DelaySendSeconds))

		return kit.Show(c, ui.RecordSpec{
			Object: view,
			Fields: []ui.Field{
				{Label: "Display Name", Value: view.DisplayName},
				{Label: "Page Size", Value: view.PageSize},
				{Label: "View Mode", Value: view.ViewMode},
				{Label: "View Layout", Value: view.ViewLayout},
				{Label: "Draft Format", Value: view.DraftType},
				{Label: "Show Moved", Value: view.ShowMoved},
				{Label: "Proton Footer", Value: view.ProtonFooter, Always: true},
				{Label: "Sign Outgoing", Value: view.Sign, Always: true},
				{Label: "Attach Public Key", Value: view.AttachKey, Always: true},
				{Label: "Auto Save Contacts", Value: view.SaveContacts, Always: true},
				{Label: "Hide Remote Images", Value: view.RemoteImages, Always: true},
				{Label: "Hide Embedded Images", Value: view.EmbeddedImages, Always: true},
				{Label: "Shortcuts", Value: view.Shortcuts, Always: true},
				{Label: "Delay Send", Value: view.DelaySend, Always: true},
				{Label: "Auto-reply", Value: view.AutoReply, Always: true},
			},
		})
	})
	c.AddCommand(addressesCmd(), foldersCmd(), labelsCmd(), filtersCmd(),
		autoreplyCmd(), forwardingCmd(), sendersCmd())
	return c
}

func secondsText(n int) string {
	if n == 0 {
		return "off"
	}
	return kit.Quantity(n, "seconds")
}

// autoReplySummary is the one-line status the settings record shows, pointing at
// the subcommand that manages it.
func autoReplySummary(raw map[string]any) string {
	if raw == nil {
		return "off"
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return "off"
	}
	ar, err := mailsvc.DecodeAutoReply(b)
	if err != nil || !ar.Enabled {
		return "off"
	}
	return "on (" + ar.ScheduleSummary() + ")"
}
