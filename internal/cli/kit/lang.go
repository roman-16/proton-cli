// Package kit is the vocabulary every command is built from: the verbs it may
// use, the argument names it may take, the way it selects what to act on, and
// the shared flag groups.
//
// It exists so that the language is declared in one place instead of emerging
// from whatever the neighbouring command happened to do. A new command that
// wants a word not listed here is a command that is inventing one, and the
// conformance test says so.
package kit

// Program is the command, and Alias is its second name: the same binary under
// the project's name, so a line written either way runs.
//
// Every screen speaks Program, whichever name was typed to get there. A help
// screen that renamed itself to match the invocation would teach two languages
// and pin nothing - the examples, the generated reference and the golden files
// all rest on the program being nameable.
const (
	Program = "proton"
	Alias   = "proton-cli"
)

// Docs is where the pages this repository carries are published, rendered and
// searchable. The site is named for the project, so it is spelled from Alias
// rather than written out again.
const Docs = "https://" + Alias + ".lerchster.dev"

// Verbs is every word that may end a command path.
//
// Each entry is the one word for its idea. Where two words competed, the winner
// is the one Proton's own interface uses; where Proton has no word, the winner is
// the one that reads as ordinary English.
var Verbs = map[string]string{
	// Reading
	"list":  "enumerate a collection",
	"get":   "show one thing in full",
	"watch": "stay attached and report each thing as it happens",

	// Writing
	"create": "make a new thing",
	"update": "change fields of an existing thing",
	"set":    "write one setting",

	// Removing
	"delete":  "remove permanently",
	"trash":   "remove reversibly",
	"restore": "undo a removal",
	"empty":   "remove everything from a trash",

	// Moving
	"move": "put into another container",
	"copy": "duplicate into another container",

	// Bytes
	"upload":   "send a local file",
	"download": "write a remote file to disk",
	"export":   "write documents to disk",
	"import":   "read documents in from disk",
	"merge":    "fold duplicates into one",

	// Mail
	"send":       "deliver a message",
	"reply":      "answer a message",
	"forward":    "pass a message on",
	"unschedule": "cancel a queued send",
	"read":       "mark as read",
	"unread":     "mark as unread",
	"label":      "attach a label",
	"unlabel":    "detach a label",
	"star":       "attach the starred label",
	"unstar":     "detach the starred label",

	// Toggles
	"enable":  "turn on",
	"disable": "turn off",

	// Sharing
	"transfer":    "hand ownership to somebody else",
	"link":        "create a public link",
	"unlink":      "remove a public link",
	"add":         "put a member into a container",
	"remove":      "take a member out of a container",
	"accept":      "agree to an invitation",
	"verify":      "confirm an address is yours",
	"resend":      "send an invitation again",
	"block":       "send a sender's mail straight to blocked",
	"allow":       "always let a sender reach the inbox",
	"spam":        "send a sender's mail straight to spam",
	"forget":      "drop a standing decision",
	"expire":      "make something delete itself later",
	"unsubscribe": "ask a mailing list to stop",
	"apply":       "run existing rules over what is already here",
	"reorder":     "set the order things run in",
	"snooze":      "take something out of the inbox until later",
	"unsnooze":    "bring something back to the inbox early",
	"decline":     "refuse an invitation",

	// Photos
	"favorite":   "mark as a favourite",
	"unfavorite": "unmark as a favourite",

	// Secrets
	"totp":     "print the code a stored secret currently stands for",
	"generate": "make a new secret",

	// Keys
	"pin":   "trust a public key for a contact",
	"unpin": "stop trusting a pinned key",

	// Calendar
	"respond": "reply to an invitation",

	// Session
	"login":  "authenticate and save a session",
	"logout": "discard a saved session",
	"revoke": "invalidate a session server-side",

	// Reference data
	"options": "list the values a choice offers",

	// The tool itself
	"changelog":  "print what each release changed",
	"report":     "collect what a bug report needs",
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
var Irreversible = map[string]bool{
	"delete": true, "empty": true, "uninstall": true,
}

// OnThisMachine marks a command that changes this computer rather than the
// account, and is the third thing declared about a mutation, beside whether it
// can be taken back and whether it changes state at all.
//
// It exists for one rule. A preview has to be as true as the change would be, so
// a dry run asserts that an account exists - otherwise it is the one path that
// answers as though it had one, since it sends no request to find out. That is
// right for every mutation that reaches Proton and wrong for the two that reach
// only the disk: replacing or deleting this binary needs no account, and asking
// for one would refuse a preview of work that would have succeeded.
//
// It is declared per command because nothing else separates them. `update` is a
// verb eight collections use, and `Updated` is the action a draft reports too;
// only the command knows whose thing it is changing.
const OnThisMachine = "on-this-machine"

// Mutating lists the verbs that change state. Every command whose verb is in
// here has to honour --dry-run, which kit.Mutate guarantees structurally.
//
// `api` is the one entry that is not obvious from its own name, and it is here
// for the reason the others are not: a raw request is whatever the caller made
// it, so the CLI cannot promise it changes nothing. A guard that has to read an
// argument before it knows whether it applies is a guard with a gap in it.
var Mutating = map[string]bool{
	"api":    true,
	"create": true, "update": true, "set": true, "delete": true, "trash": true,
	"restore": true, "empty": true, "move": true, "copy": true, "upload": true,
	"import": true, "merge": true,
	"send": true, "reply": true, "forward": true, "unschedule": true,
	"read": true, "unread": true, "label": true, "unlabel": true, "star": true,
	"unstar": true, "enable": true, "disable": true, "link": true,
	"unlink": true, "add": true, "remove": true, "accept": true,
	"decline": true, "resend": true, "block": true, "allow": true,
	"spam": true, "forget": true, "expire": true, "unsubscribe": true,
	"snooze": true, "unsnooze": true, "apply": true, "reorder": true, "favorite": true, "unfavorite": true, "pin": true,
	"unpin": true, "respond": true, "login": true, "logout": true,
	"revoke": true, "uninstall": true, "transfer": true,
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
	"calendar settings calendars": "Calendars",
	"mail settings addresses":     "Identity and addresses",
	"mail settings autoreply":     "Forward and auto-reply",
	"mail settings filters":       "Filters",
	"mail settings folders":       "Folders and labels",
	"mail settings labels":        "Folders and labels",
	"mail settings senders":       "Spam, block, and allow lists",
	"pass settings domains":       "Aliases",
	"pass settings mailboxes":     "Aliases",
}

// Placeholders is every argument name a usage string may contain.
//
// One name per idea, so `REF` means the same thing in every command that takes
// one: a full ID, an eight-character short ID, or a human handle such as a
// subject, a name, a path or an email address.
var Placeholders = map[string]string{
	"REF":            "a full ID, a short ID, or a human handle",
	"PATH":           "a Drive path that does not exist yet",
	"SRC":            "a local file or directory to read",
	"DEST":           "a Drive folder to write into",
	"EMAIL":          "an email address",
	"NEW_NAME":       "the name to change something to",
	"KEY":            "a setting key",
	"VALUE":          "a setting value",
	"METHOD":         "an HTTP method",
	"ENDPOINT":       "a Proton API path",
	"VERSION":        "a " + Program + " release, as X.Y.Z",
	"ATTACHMENT_REF": "an attachment on the addressed message",
	"REVISION_REF":   "a revision of the addressed file",
	"CONTACT_REF":    "a contact, when the command already addresses something else",
	"PHOTO_REF":      "a photo, when the command already addresses an album",
}
