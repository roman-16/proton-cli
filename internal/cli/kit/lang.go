package kit

import "github.com/roman-16/proton-cli/internal/errs"

const (
	Program = "proton"
	Alias   = "proton-cli"
)

const Docs = "https://" + Alias + ".lerchster.dev"

// Verbs is every word that may end a command path.
//
// Each entry is the one word for its idea, and each is spoken: a word kept here
// after the command that said it is gone describes a CLI that no longer exists,
// and the next reader trusts it anyway. Where two words competed, the winner
// is the one Proton's own interface uses; where Proton has no word, the winner is
// the one that reads as ordinary English.
var Verbs = map[string]string{
	"list":  "enumerate a collection",
	"get":   "show one thing in full",
	"count": "say how many there are in each place, and how many are unread",
	"watch": "stay attached and report each thing as it happens",

	"create": "make a new thing",
	"update": "change fields of an existing thing",
	"set":    "write one setting",

	"delete":  "remove permanently",
	"trash":   "remove reversibly",
	"restore": "undo a removal",
	"empty":   "remove everything from a trash",
	"undo":    "take back everything a piece of work brought in",

	// Work Proton carries out over time
	"cancel": "stop work that is under way",
	"resume": "set stopped work going again",

	"move": "put into another container",
	"copy": "duplicate into another container",

	"upload":   "send a local file",
	"download": "write a remote file to disk",
	"export":   "write documents to disk",
	"import":   "read documents in from disk",
	"merge":    "fold duplicates into one",

	"send":       "deliver a message",
	"reply":      "answer a message",
	"forward":    "pass a message on",
	"unschedule": "cancel a queued send",
	"read":       "mark as read",
	"unread":     "mark as unread",
	"receipt":    "tell a sender their message was read",
	"legitimate": "overrule a flagged message's verdict",
	"phishing":   "report a message to Proton as phishing",
	"label":      "attach a label",
	"unlabel":    "detach a label",
	"star":       "attach the starred label",
	"unstar":     "detach the starred label",

	"enable":  "turn on",
	"disable": "turn off",

	"abuse":       "report something shared with you to Proton as abuse",
	"transfer":    "hand ownership to somebody else",
	"add":         "put a member into a container",
	"remove":      "take a member out of a container",
	"confirm":     "hand the key to somebody invited before they had a Proton account",
	"leave":       "give up access somebody granted you",
	"accept":      "agree to an invitation",
	"verify":      "confirm an address is yours",
	"resend":      "send an invitation again",
	"block":       "send a sender's mail straight to blocked",
	"allow":       "always let a sender reach the inbox",
	"spam":        "send a sender's mail straight to spam",
	"unsubscribe": "ask a mailing list to stop",
	"apply":       "run existing rules over what is already here",
	"reorder":     "set the order things are kept in",
	"snooze":      "take something out of the inbox until later",
	"unsnooze":    "bring something back to the inbox early",
	"decline":     "refuse an invitation",

	"resolve": "mark a breach as dealt with",
	"reopen":  "mark a resolved breach as open again",

	"favorite":   "mark as a favourite",
	"unfavorite": "unmark as a favourite",

	"pin":     "keep at the top of a list",
	"unpin":   "stop keeping at the top of a list",
	"hide":    "leave out of every listing that does not name it",
	"unhide":  "bring back into the listings",
	"exclude": "leave out of the checks Pass Monitor runs",
	"include": "bring back into the checks Pass Monitor runs",

	"totp":     "print the code a stored secret currently stands for",
	"generate": "make a new secret",

	"trust":      "encrypt to a public key and verify with it, for a contact",
	"untrust":    "stop relying on a contact's trusted keys",
	"reactivate": "bring keys a password reset locked back into use",

	"request": "start the wait to use emergency access somebody granted you",
	"access":  "sign in to an account whose emergency access is now open",
	"grant":   "let a pending emergency request in now, without the wait",

	"respond": "reply to an invitation",

	"login":  "authenticate and save a session",
	"logout": "discard a saved session",
	"revoke": "stop something handed out working: a session, a public link",
	"rotate": "replace a link with a new one, which stops the old one working",

	"changelog":  "print what each release changed",
	"report":     "collect what a bug report needs",
	"skill":      "emit the instructions an AI agent reads before using " + Program,
	"uninstall":  "remove " + Program,
	"version":    "print the build",
	"completion": "emit a shell completion script",
	"api":        "send a raw authenticated request",
}

// Irreversible lists the verbs whose work neither this CLI nor Proton's own
// clients can take back. Every command whose verb is in here stops for a yes,
// which kit.Mutate guarantees structurally from the action it reports.
//
// `uninstall` belongs beside the two removals because it is the strictest case
// of the same thing: afterwards there is no proton left to undo it with.
//
// `leave` is here for what it takes back: only the person who shared the thing
// can grant it again, so nothing this account does undoes it.
//
// `api` is here for the reason it is a mutation: a raw request is whatever the
// caller made it. Nothing on the command line says which requests destroy - the
// endpoint that permanently deletes a message is a PUT - so a policy that read
// the method would be a fence with the one thing it was put up for on the far
// side of it.
var Irreversible = map[string]bool{
	"api": true, "delete": true, "empty": true, "leave": true, "uninstall": true,
	"undo": true,
}

var Opaque = map[string]bool{"api": true}

const SignedOut = "signed-out"

var Mutating = map[string]bool{
	"api":    true,
	"create": true, "update": true, "set": true, "delete": true, "trash": true,
	"restore": true, "empty": true, "move": true, "copy": true, "upload": true,
	"import": true, "merge": true, "undo": true,
	"cancel": true, "resume": true,
	"send": true, "reply": true, "forward": true, "unschedule": true, "receipt": true,
	"legitimate": true, "phishing": true,
	"read": true, "unread": true, "label": true, "unlabel": true, "star": true,
	"unstar": true, "enable": true, "disable": true,
	"add": true, "remove": true, "confirm": true,
	"leave": true, "accept": true,
	"decline": true, "resend": true, "block": true, "allow": true,
	"spam": true, "unsubscribe": true,
	"snooze": true, "unsnooze": true, "apply": true, "reorder": true, "favorite": true, "unfavorite": true, "pin": true,
	"unpin": true, "hide": true, "unhide": true, "exclude": true, "include": true,
	"trust": true, "untrust": true, "reactivate": true, "respond": true, "login": true, "logout": true,
	"revoke": true, "rotate": true, "uninstall": true, "transfer": true, "abuse": true,
	"grant": true, "request": true, "access": true,
	"resolve": true, "reopen": true,
}

// SettingsPages declares every collection that lives under a `settings` group,
// and the page of Proton's own settings app it mirrors.
//
// Where a collection hangs is the one structural question this CLI answers over
// and over, and it is not a matter of taste. Proton's clients already answer it:
// what the product app itself creates and edits is a working surface, and what
// only the settings app reaches is a setting. A vault, a contact group and a
// photo album have no settings page at all, so they sit at app level; a filter
// and an address have no place in the mail window, so they sit under `settings`.
//
// Folders, labels and calendars are the interesting case: Proton offers all
// three in both places, in the sidebar and again as a settings page. The tie
// goes to `settings`, because a CLI has no sidebar and the settings page is the
// one that enumerates them.
//
// The value is the page, not a boolean, so the entry carries its own reason and
// a new collection cannot be filed here without someone having looked.
var SettingsPages = map[string]string{
	"account settings password":          "Account and password",
	"account settings emergency-access":  "Recovery",
	"account settings recovery-contacts": "Recovery",
	"account settings recovery-email":    "Recovery",
	"account settings recovery-phone":    "Recovery",
	"account settings recovery-phrase":   "Recovery",
	"account settings notifications":     "Subscription",
	"account settings second-password":   "Account and password",
	"account settings security-keys":     "Account and password",
	"account settings sentinel":          "Security and privacy",
	"account settings two-factor":        "Account and password",
	"calendar settings calendars":        "Calendars",
	"calendar settings holidays":         "Calendars",
	"calendar settings links":            "Calendars",
	"mail settings addresses":            "Identity and addresses",
	"mail settings autoreply":            "Forward and auto-reply",
	"mail settings categories":           "Messages and composing",
	"mail settings domains":              "Domain names",
	"mail settings filters":              "Filters",
	"mail settings forwarding":           "Forward and auto-reply",
	"mail settings folders":              "Folders and labels",
	"mail settings imports":              "Import via Easy Switch",
	"mail settings labels":               "Folders and labels",
	"mail settings senders":              "Spam, block, and allow lists",
	"mail settings smtp-tokens":          "IMAP/SMTP",
	"pass settings access-tokens":        "Access tokens",
	"pass settings domains":              "Aliases",
	"pass settings extra-password":       "Security",
	"pass settings mailboxes":            "Aliases",
}

type Placeholder struct {
	Means string
	Picks string
	Check func(value string) *errs.Problem
}

const TakesNone = "takes-none"

const (
	PicksAddressed = "the collection this command acts on"
	PicksHolding   = "the collection this command is part of"
)

const Addresses = "addresses"

var Placeholders = map[string]Placeholder{
	"ALIAS_CONTACT_REF": {Means: "an address the addressed alias may write to", Picks: PicksHolding},
	"ATTACHMENT_REF":    {Means: "an attachment on the addressed message or item", Picks: PicksHolding},
	"COMMAND":           {Means: "a command, as you would type it"},
	"CODE":              {Means: "a sign-in code another device is showing"},
	"CONTACT_REF":       {Means: "a contact, when the command already addresses something else", Picks: "contacts"},
	"DEST":              {Means: "a Drive folder to write into"},
	"DOMAIN":            {Means: "a domain name"},
	"EMAIL":             {Means: "an email address", Check: address},
	"ENDPOINT":          {Means: "a Proton API path"},
	"KEY":               {Means: "a setting key"},
	"LINK":              {Means: "a password-protected message, by the link you were sent or the id in it"},
	"LOGIN":             {Means: "what signs in to another mailbox, usually its address"},
	"METHOD":            {Means: "an HTTP method"},
	"NEW_NAME":          {Means: "the name to change something to"},
	"PASSKEY_REF":       {Means: "a passkey stored against the addressed login", Picks: PicksHolding},
	"PATH":              {Means: "a Drive path that does not exist yet"},
	"PHONE":             {Means: "a phone number, with its country code"},
	"PHOTO_REF":         {Means: "a photo, when the command already addresses an album", Picks: "drive photos"},
	"REF":               {Means: "a full ID, a short ID, or a human handle", Picks: PicksAddressed},
	"REVISION_REF":      {Means: "a revision of the addressed file or item", Picks: PicksHolding},
	"SENDER":            {Means: "an email address, or a whole domain written as @example.com", Check: sender},
	"SHELL":             {Means: "a shell to write the script for"},
	"SRC":               {Means: "a local file or directory to read"},
	"URL":               {Means: "a public link, as it was sent to you"},
	"VALUE":             {Means: "a setting value"},
	"VERSION":           {Means: "a " + Program + " release, as X.Y.Z"},
}
