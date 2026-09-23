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

const (
	pageComposing = "Messages and composing"
	pageFilters   = "Filters"
	pageFolders   = "Folders and labels"
	pageIdentity  = "Identity and addresses"
	pageKeys      = "Encryption and keys"
	pagePrivacy   = "Email privacy"
)

const (
	storeRemoteImages = 1
	proxyRemoteImages = 2
)

const (
	defaultFontFace = "Arial"
	defaultFontSize = 14
)

var specs = map[string]kit.Setting{
	"almost-all-mail": {
		Path: settingsPath + "/almost-all-mail", Field: "AlmostAllMail",
		Page: pageComposing, Desc: "Exclude spam and trash from All mail", Enum: kit.OnOffNumbers(),
	},
	"attach-public-key": {
		Path: settingsPath + "/attachpublic", Field: "AttachPublicKey",
		Page: pageKeys, Desc: "Attach your public key to outgoing mail", Enum: kit.OnOffNumbers(),
	},
	"auto-delete-spam-trash": {
		Path: settingsPath + "/auto-delete-spam-and-trash-days", Field: "Days",
		Page: pageComposing, Desc: "Delete spam and trash permanently after this long",
		Enum: []kit.Choice{{Name: "off", Value: 0}, {Name: "30d", Value: 30}},
	},
	"auto-save-contacts": {
		Path: settingsPath + "/autocontacts", Field: "AutoSaveContacts",
		Page: pageComposing, Desc: "Add unknown recipients to Contacts", Enum: kit.OnOffNumbers(),
	},
	"block-sender-confirmation": {
		Path: settingsPath + "/block-sender-confirmation", Field: "BlockSenderConfirmation",
		Page: pageFilters, Desc: "Ask before blocking a sender in the web client",
		Enum: []kit.Choice{{Name: "off", Value: 1}, {Name: "on", Value: nil}},
	},
	"category-view": {
		Path: settingsPath + "/mail-category-view", Field: "MailCategoryView",
		Page: pageComposing, Desc: "Sort the inbox into category tabs", Enum: kit.OnOffBooleans(),
	},
	"category-view-counters": {
		Path: settingsPath + "/mail-category-view-counters-enabled", Field: "MailCategoryViewCountersEnabled",
		Page: pageComposing, Desc: "Show unread counts on the category tabs", Enum: kit.OnOffBooleans(),
	},
	"composer-mode": {
		Path: settingsPath + "/composermode", Field: "ComposerMode",
		Page: pageComposing, Desc: "How the web composer opens",
		Enum: kit.Ordered("popup", "maximized"),
	},
	"confirm-link": {
		Path: settingsPath + "/confirmlink", Field: "ConfirmLink",
		Page: pageComposing, Desc: "Confirm before opening an external link", Enum: kit.OnOffNumbers(),
	},
	"delay-send": {
		Path: settingsPath + "/delaysend", Field: "DelaySendSeconds",
		Page: pageComposing, Desc: "How long the undo-send window lasts",
		Range: &kit.IntRange{Min: 0, Max: 20, Unit: "seconds"},
	},
	"draft-type": {
		Path: settingsPath + "/drafttype", Field: "MIMEType",
		Page: pageComposing, Desc: "Default composer format",
		Enum: []kit.Choice{{Name: "text/html", Value: "text/html"}, {Name: "text/plain", Value: "text/plain"}},
	},
	"enable-folder-color": {
		Path: settingsPath + "/enablefoldercolor", Field: "EnableFolderColor",
		Page: pageFolders, Desc: "Colour folders in the sidebar", Enum: kit.OnOffNumbers(),
	},
	"font-face": {
		Path: settingsPath + "/fontface", Field: "FontFace",
		Page: pageComposing, Desc: "Default font in the web composer",
		Enum: []kit.Choice{
			{Name: "arial", Value: "Arial"},
			{Name: "georgia", Value: "Georgia"},
			{Name: "helvetica", Value: "Helvetica"},
			{Name: "monospace", Value: "Menlo, Consolas, Courier New, Monospace"},
			{Name: "sans-serif", Value: "Sans-serif"},
			{Name: "serif", Value: "Serif"},
			{Name: "tahoma", Value: "Tahoma, sans-serif"},
			{Name: "times-new-roman", Value: "Times New Roman"},
			{Name: "trebuchet-ms", Value: "Trebuchet MS"},
			{Name: "verdana", Value: "Verdana"},
		},
	},
	"font-size": {
		Path: settingsPath + "/fontsize", Field: "FontSize",
		Page: pageComposing, Desc: "Default font size in the web composer, in pixels",
		Enum: []kit.Choice{
			{Name: "10", Value: 10}, {Name: "12", Value: 12}, {Name: "14", Value: 14},
			{Name: "16", Value: 16}, {Name: "18", Value: 18}, {Name: "20", Value: 20},
			{Name: "22", Value: 22}, {Name: "24", Value: 24}, {Name: "26", Value: 26},
		},
	},
	"hide-embedded-images": {
		Path: settingsPath + "/hide-embedded-images", Field: "HideEmbeddedImages",
		Page: pageComposing, Desc: "Block images embedded in messages", Enum: kit.OnOffNumbers(),
	},
	"hide-remote-images": {
		Path: settingsPath + "/hide-remote-images", Field: "HideRemoteImages",
		Page: pagePrivacy, Desc: "Block images loaded from the internet", Enum: kit.OnOffNumbers(),
	},
	"hide-sender-images": {
		Path: settingsPath + "/hide-sender-images", Field: "HideSenderImages",
		Page: pageComposing, Desc: "Block sender profile pictures", Enum: kit.OnOffNumbers(),
	},
	"image-proxy": {
		Path: settingsPath + "/imageproxy", Field: "ImageProxy",
		Page: pagePrivacy, Desc: "Block email tracking in remote images and links",
		Enum: []kit.Choice{
			{Name: "off", Value: 0, Bodies: []map[string]any{
				{"ImageProxy": storeRemoteImages, "Action": 0},
				{"ImageProxy": proxyRemoteImages, "Action": 0},
			}},
			{Name: "on", Value: proxyRemoteImages, Bodies: []map[string]any{
				{"ImageProxy": proxyRemoteImages, "Action": 1},
			}},
		},
	},
	"inherit-folder-color": {
		Path: settingsPath + "/inheritparentfoldercolor", Field: "InheritParentFolderColor",
		Page: pageFolders, Desc: "Subfolders inherit their parent's colour", Enum: kit.OnOffNumbers(),
	},
	"page-size": {
		Path: settingsPath + "/pagesize", Field: "PageSize",
		Page: pageComposing, Desc: "Messages per page in the web client",
		Enum: []kit.Choice{{Name: "50", Value: 50}, {Name: "100", Value: 100}, {Name: "200", Value: 200}},
	},
	"pm-signature": {
		Path: settingsPath + "/pmsignature", Field: "PMSignature",
		Page: pageIdentity, Desc: `Append "Sent with Proton Mail secure email."`, Enum: kit.OnOffNumbers(),
	},
	"prompt-pin": {
		Path: settingsPath + "/promptpin", Field: "PromptPin",
		Page: pageKeys, Desc: "Offer to pin the keys of contacts who sign their mail",
		Enum: kit.OnOffNumbers(),
	},
	"shortcuts": {
		Path: settingsPath + "/shortcuts", Field: "Shortcuts",
		Page: pageComposing, Desc: "Keyboard shortcuts in the web client", Enum: kit.OnOffNumbers(),
	},
	"show-moved": {
		Path: settingsPath + "/moved", Field: "ShowMoved",
		Page: pageComposing, Desc: "Keep moved drafts and sent mail in their folders",
		Enum: kit.Ordered("none", "drafts", "sent", "drafts-and-sent"),
	},
	"sign": {
		Path: settingsPath + "/sign", Field: "Sign",
		Page: pageKeys, Desc: "Sign outgoing mail by default", Enum: kit.OnOffNumbers(),
	},
	"sticky-labels": {
		Path: settingsPath + "/stickylabels", Field: "StickyLabels",
		Page: pageComposing, Desc: "Keep a label when moving a message", Enum: kit.OnOffNumbers(),
	},
	"unread-favicon": {
		Path: settingsPath + "/unread-favicon", Field: "UnreadFavicon",
		Page: pageComposing, Desc: "Show the unread count in the browser tab", Enum: kit.OnOffNumbers(),
	},
	"view-layout": {
		Path: settingsPath + "/viewlayout", Field: "ViewLayout",
		Page: pageComposing, Desc: "Mailbox layout", Enum: kit.Ordered("column", "row"),
	},
	"next-message-on-move": {
		Path: settingsPath + "/next-message-on-move", Field: "NextMessageOnMove",
		Page: pageComposing, Desc: "Open the next message after moving one", Enum: kit.OnOffNumbers(),
	},
	"pgp-scheme": {
		Path: settingsPath + "/pgpscheme", Field: "PGPScheme",
		Page: pageKeys, Desc: "How mail to external PGP recipients is packaged",
		// Proton stores these as the package-type bits they select.
		Enum: []kit.Choice{{Name: "pgp-mime", Value: 16}, {Name: "pgp-inline", Value: 8}},
	},
	"remove-image-metadata": {
		Path: settingsPath + "/remove-image-metadata", Field: "RemoveImageMetadata",
		Page: pageComposing, Desc: "Strip EXIF and location from images you attach",
		Enum: kit.OnOffBooleans(),
	},
	"right-to-left": {
		Path: settingsPath + "/righttoleft", Field: "RightToLeft",
		Page: pageComposing, Desc: "Compose right to left",
		Enum: kit.Ordered("left-to-right", "right-to-left"),
	},
	"spam-action": {
		Path: settingsPath + "/spam-action", Field: "SpamAction",
		Page: pageComposing, Desc: "What moving to spam also does",
		Enum: []kit.Choice{
			{Name: "just-move", Value: 0}, {Name: "move-and-unsubscribe", Value: 1}, {Name: "ask", Value: nil},
		},
	},
	"view-mode": {
		Path: settingsPath + "/viewmode", Field: "ViewMode",
		Page: pageComposing, Desc: "Group mail into threads or list single messages",
		Enum: kit.Ordered("conversations", "messages"),
	},
}

// settingsView is the shape `mail settings get` reports: declared, snake_case, and
// speaking the same value names `set` accepts.
type settingsView struct {
	HideRemoteImages        string `json:"hide_remote_images"`
	ImageProxy              string `json:"image_proxy"`
	AttachPublicKey         string `json:"attach_public_key"`
	PGPScheme               string `json:"pgp_scheme"`
	PromptPin               string `json:"prompt_pin"`
	Sign                    string `json:"sign"`
	BlockSenderConfirmation string `json:"block_sender_confirmation"`
	EnableFolderColor       string `json:"enable_folder_color"`
	InheritFolderColor      string `json:"inherit_folder_color"`
	PMSignature             string `json:"pm_signature"`
	AlmostAllMail           string `json:"almost_all_mail"`
	AutoDeleteSpamTrash     string `json:"auto_delete_spam_trash"`
	AutoSaveContacts        string `json:"auto_save_contacts"`
	CategoryView            string `json:"category_view"`
	CategoryViewCounters    string `json:"category_view_counters"`
	ComposerMode            string `json:"composer_mode"`
	ConfirmLink             string `json:"confirm_link"`
	DelaySend               string `json:"delay_send"`
	DraftType               string `json:"draft_type"`
	FontFace                string `json:"font_face"`
	FontSize                string `json:"font_size"`
	HideEmbeddedImages      string `json:"hide_embedded_images"`
	HideSenderImages        string `json:"hide_sender_images"`
	NextMessageOnMove       string `json:"next_message_on_move"`
	PageSize                string `json:"page_size"`
	RemoveImageMetadata     string `json:"remove_image_metadata"`
	RightToLeft             string `json:"right_to_left"`
	Shortcuts               string `json:"shortcuts"`
	ShowMoved               string `json:"show_moved"`
	SpamAction              string `json:"spam_action"`
	StickyLabels            string `json:"sticky_labels"`
	UnreadFavicon           string `json:"unread_favicon"`
	ViewLayout              string `json:"view_layout"`
	ViewMode                string `json:"view_mode"`
	DisplayName             string `json:"display_name,omitempty"`
	AutoReply               string `json:"auto_reply"`
}

type storedSettings struct {
	AlmostAllMail                   any
	AttachPublicKey                 any
	AutoDeleteSpamAndTrashDays      any
	AutoResponder                   map[string]any
	AutoSaveContacts                any
	BlockSenderConfirmation         any
	ComposerMode                    any
	ConfirmLink                     any
	DelaySendSeconds                any
	DisplayName                     string
	DraftMIMEType                   any
	EnableFolderColor               any
	FontFace                        any
	FontSize                        any
	HideEmbeddedImages              any
	HideRemoteImages                any
	HideSenderImages                any
	ImageProxy                      any
	InheritParentFolderColor        any
	MailCategoryView                any
	MailCategoryViewCountersEnabled any
	NextMessageOnMove               any
	PageSize                        any
	PGPScheme                       any
	PMSignature                     any
	PromptPin                       any
	RemoveImageMetadata             any
	RightToLeft                     any
	Shortcuts                       any
	ShowMoved                       any
	Sign                            any
	SpamAction                      any
	StickyLabels                    any
	UnreadFavicon                   any
	ViewLayout                      any
	ViewMode                        any
}

func (m storedSettings) view() settingsView {
	name := func(key string, v any) string { return specs[key].Name(v) }
	toggle := func(v any) string { return kit.OnOffText(boolInt(kit.IntOf(v) != 0)) }
	blockSender := m.BlockSenderConfirmation
	if kit.IntOf(blockSender) != 1 {
		blockSender = nil
	}
	return settingsView{
		HideRemoteImages:        toggle(m.HideRemoteImages),
		ImageProxy:              toggle(kit.IntOf(m.ImageProxy) & proxyRemoteImages),
		AttachPublicKey:         toggle(m.AttachPublicKey),
		PGPScheme:               name("pgp-scheme", m.PGPScheme),
		PromptPin:               toggle(m.PromptPin),
		Sign:                    toggle(m.Sign),
		BlockSenderConfirmation: name("block-sender-confirmation", blockSender),
		EnableFolderColor:       toggle(m.EnableFolderColor),
		InheritFolderColor:      toggle(m.InheritParentFolderColor),
		// PMSignature carries more than one bit; only the low one says whether
		// the footer is appended.
		PMSignature:          toggle(kit.IntOf(m.PMSignature) & 1),
		AlmostAllMail:        toggle(m.AlmostAllMail),
		AutoDeleteSpamTrash:  name("auto-delete-spam-trash", kit.IntOf(m.AutoDeleteSpamAndTrashDays)),
		AutoSaveContacts:     toggle(m.AutoSaveContacts),
		CategoryView:         toggle(m.MailCategoryView),
		CategoryViewCounters: toggle(m.MailCategoryViewCountersEnabled),
		ComposerMode:         name("composer-mode", m.ComposerMode),
		ConfirmLink:          toggle(m.ConfirmLink),
		DelaySend:            secondsText(kit.IntOf(m.DelaySendSeconds)),
		DraftType:            name("draft-type", m.DraftMIMEType),
		FontFace:             name("font-face", orDefault(m.FontFace, defaultFontFace)),
		FontSize:             name("font-size", orDefault(m.FontSize, defaultFontSize)),
		HideEmbeddedImages:   toggle(m.HideEmbeddedImages),
		HideSenderImages:     toggle(m.HideSenderImages),
		NextMessageOnMove:    toggle(m.NextMessageOnMove),
		PageSize:             name("page-size", m.PageSize),
		RemoveImageMetadata:  toggle(m.RemoveImageMetadata),
		RightToLeft:          name("right-to-left", m.RightToLeft),
		Shortcuts:            toggle(m.Shortcuts),
		ShowMoved:            name("show-moved", m.ShowMoved),
		SpamAction:           name("spam-action", m.SpamAction),
		StickyLabels:         toggle(m.StickyLabels),
		UnreadFavicon:        toggle(m.UnreadFavicon),
		ViewLayout:           name("view-layout", m.ViewLayout),
		ViewMode:             name("view-mode", m.ViewMode),
		DisplayName:          m.DisplayName,
		AutoReply:            autoReplySummary(m.AutoResponder),
	}
}

func orDefault(v, fallback any) any {
	switch x := v.(type) {
	case nil:
		return fallback
	case string:
		if x == "" {
			return fallback
		}
	case float64:
		if x == 0 {
			return fallback
		}
	}
	return v
}

func (v settingsView) fields() []ui.Field {
	return []ui.Field{
		{Label: "Hide Remote Images", Value: v.HideRemoteImages, Always: true},
		{Label: "Image Proxy", Value: v.ImageProxy, Always: true},
		{Label: "Attach Public Key", Value: v.AttachPublicKey, Always: true},
		{Label: "PGP Scheme", Value: v.PGPScheme, Always: true},
		{Label: "Prompt Pin", Value: v.PromptPin, Always: true},
		{Label: "Sign", Value: v.Sign, Always: true},
		{Label: "Block Sender Confirmation", Value: v.BlockSenderConfirmation, Always: true},
		{Label: "Enable Folder Color", Value: v.EnableFolderColor, Always: true},
		{Label: "Inherit Folder Color", Value: v.InheritFolderColor, Always: true},
		{Label: "PM Signature", Value: v.PMSignature, Always: true},
		{Label: "Almost All Mail", Value: v.AlmostAllMail, Always: true},
		{Label: "Auto Delete Spam Trash", Value: v.AutoDeleteSpamTrash, Always: true},
		{Label: "Auto Save Contacts", Value: v.AutoSaveContacts, Always: true},
		{Label: "Category View", Value: v.CategoryView, Always: true},
		{Label: "Category View Counters", Value: v.CategoryViewCounters, Always: true},
		{Label: "Composer Mode", Value: v.ComposerMode, Always: true},
		{Label: "Confirm Link", Value: v.ConfirmLink, Always: true},
		{Label: "Delay Send", Value: v.DelaySend, Always: true},
		{Label: "Draft Type", Value: v.DraftType, Always: true},
		{Label: "Font Face", Value: v.FontFace, Always: true},
		{Label: "Font Size", Value: v.FontSize, Always: true},
		{Label: "Hide Embedded Images", Value: v.HideEmbeddedImages, Always: true},
		{Label: "Hide Sender Images", Value: v.HideSenderImages, Always: true},
		{Label: "Next Message On Move", Value: v.NextMessageOnMove, Always: true},
		{Label: "Page Size", Value: v.PageSize, Always: true},
		{Label: "Remove Image Metadata", Value: v.RemoveImageMetadata, Always: true},
		{Label: "Right To Left", Value: v.RightToLeft, Always: true},
		{Label: "Shortcuts", Value: v.Shortcuts, Always: true},
		{Label: "Show Moved", Value: v.ShowMoved, Always: true},
		{Label: "Spam Action", Value: v.SpamAction, Always: true},
		{Label: "Sticky Labels", Value: v.StickyLabels, Always: true},
		{Label: "Unread Favicon", Value: v.UnreadFavicon, Always: true},
		{Label: "View Layout", Value: v.ViewLayout, Always: true},
		{Label: "View Mode", Value: v.ViewMode, Always: true},
		{Label: "Display Name", Value: v.DisplayName},
		{Label: "Auto-reply", Value: v.AutoReply, Always: true},
	}
}

func settingsCmd() *cobra.Command {
	c := kit.Settings("mail", "How Mail behaves", specs, settingsView{}, func(c *kit.Invocation) error {
		resp, err := c.App.API.Do(c.Ctx, proton.Request{Method: "GET", Path: settingsPath})
		if err != nil {
			return err
		}
		var env struct{ MailSettings storedSettings }
		if err := json.Unmarshal(resp.Body, &env); err != nil {
			return err
		}
		view := env.MailSettings.view()
		return kit.Show(c, ui.RecordSpec{Object: view, Fields: view.fields()})
	})
	c.AddCommand(addressesCmd(), categoriesCmd(), domainsCmd(), foldersCmd(), importsCmd(), labelsCmd(),
		filtersCmd(), autoreplyCmd(), forwardingCmd(), sendersCmd(), smtpTokensCmd())
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
