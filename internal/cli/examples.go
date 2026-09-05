package cli

import (
	"strings"

	"github.com/spf13/cobra"
)

// Every command, shown being used.
//
// The grammar is this CLI's whole premise: one shape, learned once, then guessed
// correctly everywhere else. `--help` is where that learning happens, and a
// screen that lists twenty flags without once showing the sentence they belong
// to leaves the reader to assemble it from parts.
//
// They live here rather than beside each command for the same reason the verbs
// and the placeholders do: the examples are the language being spoken, and a
// language is easier to keep consistent when you can read it in one sitting. The
// conformance test parses every line against the real tree, so an example cannot
// name a command that does not exist, use a flag that was renamed, or illustrate
// a different command from the one it is filed under.
//
// The values are deliberately the same cast throughout - Jane Roe, invoice 2291,
// /Documents, a Work label - so the examples read as one account being used
// rather than as a hundred unrelated fragments.
var examples = map[string][]string{
	// ── account ──
	"proton account get": {
		"proton account get",
		"proton account get --output json",
	},
	"proton account login": {
		"proton account login",
		"proton account login --profile work",
		"proton account login --user me@proton.me --password-file /run/secrets/proton",
		"proton account login --user me@proton.me --password-stdin --totp 123456",
		"proton account login --user me@proton.me --password-file /run/secrets/proton --second-password-file /run/secrets/proton-second",
		"proton account login --user me@proton.me --password-file /run/secrets/proton --extra-password-file /run/secrets/proton-pass",
	},
	"proton account logout": {
		"proton account logout",
		"proton account logout --revoke",
		"proton account logout --all",
	},
	"proton account profiles list":   {"proton account profiles list"},
	"proton account profiles delete": {"proton account profiles delete work"},
	"proton account sessions list":   {"proton account sessions list"},
	"proton account sessions revoke": {
		"proton account sessions revoke 5bH2mQxK",
		"proton account sessions revoke --others",
	},
	"proton account settings get":  {"proton account settings get"},
	"proton account settings list": {"proton account settings list"},
	"proton account settings set": {
		"proton account settings set locale de_AT",
		"proton account settings set news off",
	},
	"proton api": {
		"proton api GET /core/v4/users",
		"proton api GET /mail/v4/messages --query 'PageSize=5'",
		"proton api POST /mail/v4/labels --body '{\"Name\":\"Work\",\"Color\":\"#8080FF\",\"Type\":1}'",
	},

	// ── calendar ──
	"proton calendar events create": {
		"proton calendar events create --title Dentist --start 2026-04-16T14:00 --duration 1h",
		"proton calendar events create --title Standup --start 2026-04-16T09:00 --duration 15m --rrule 'FREQ=WEEKLY;COUNT=10' --remind 15m",
		"proton calendar events create --title Holiday --start 2026-07-01 --all-day --calendar Personal",
		"proton calendar events create --title 'Design review' --start 2026-04-20T10:00 --end 2026-04-20T10:45 --attendee jane@example.com --location 'Room 3'",
		"proton calendar events create --title Renewal --start 2026-09-01T09:00 --remind 1d:email",
		"proton calendar events create --title Deadline --start 2026-04-30T17:00 --duration 1h --color strawberry",
	},
	"proton calendar events export": {
		"proton calendar events export --start 2026-01-01 --end 2026-12-31 --dest year.ics",
		"proton calendar events export --calendar Work --dest - > work.ics",
	},
	"proton calendar events import": {
		"proton calendar events import holidays.ics",
		"proton calendar events import --calendar Work team.ics",
		"curl -s https://example.com/team.ics | proton calendar events import -",
	},
	"proton calendar events list": {
		"proton calendar events list",
		"proton calendar events list --start 2026-04-15 --end 2026-04-30",
		"proton calendar events list --calendar Work",
	},
	"proton calendar reminders list": {
		"proton calendar reminders list",
		"proton calendar reminders list --start 2026-04-20 --end 2026-04-21",
		"proton calendar reminders list --calendar Work --output json",
	},
	"proton calendar reminders watch": {
		"proton calendar reminders watch",
		"proton calendar reminders watch --calendar Work",
		"proton calendar reminders watch --output json",
	},
	"proton calendar events get": {
		"proton calendar events get Dentist",
		"proton calendar events get 4f2a1b9c@2026-04-22T09:00",
	},
	"proton calendar events update": {
		"proton calendar events update Dentist --start 2026-04-16T15:30",
		"proton calendar events update 4f2a1b9c@2026-04-22T09:00 --location 'Room 3'",
		"proton calendar events update 4f2a1b9c@2026-04-22T09:00 --title Standup --onwards",
		"proton calendar events update Offsite --all-day",
		"proton calendar events update Offsite --all-day=false --start 2026-07-01T09:00 --end 2026-07-01T17:00",
		"proton calendar events update Dentist --color pacific",
	},
	"proton calendar events delete": {
		"proton calendar events delete Dentist",
		"proton calendar events delete 4f2a1b9c@2026-05-04T09:00 --onwards",
	},
	"proton calendar events respond": {
		"proton calendar events respond 'Team sync' --answer accept",
		"proton calendar events respond 'Team sync' --answer decline",
	},
	"proton calendar settings calendars list": {"proton calendar settings calendars list"},
	"proton calendar settings calendars get": {
		"proton calendar settings calendars get Work",
	},
	"proton calendar settings calendars create": {
		"proton calendar settings calendars create --name Work",
		"proton calendar settings calendars create --name Personal --color pacific",
		"proton calendar settings calendars create --name Timetable --url https://example.com/team.ics",
	},
	"proton calendar invitations list": {
		"proton calendar invitations list",
	},
	"proton calendar invitations accept": {
		"proton calendar invitations accept Work",
	},
	"proton calendar invitations decline": {
		"proton calendar invitations decline Work",
	},
	"proton calendar settings calendars share add": {
		"proton calendar settings calendars share add Work jane@proton.me",
		"proton calendar settings calendars share add Work jane@proton.me --edit",
	},
	"proton calendar settings calendars share list": {
		"proton calendar settings calendars share list Work",
	},
	"proton calendar settings calendars share remove": {
		"proton calendar settings calendars share remove Work jane@proton.me",
	},
	"proton calendar settings calendars update": {
		"proton calendar settings calendars update Work --name Office",
		"proton calendar settings calendars update Work --color enzian",
		"proton calendar settings calendars update Work --default-duration 30m --remind 15m",
		"proton calendar settings calendars update Personal --busy off",
	},
	"proton calendar settings calendars delete": {"proton calendar settings calendars delete Work"},
	"proton calendar settings get":              {"proton calendar settings get"},
	"proton calendar settings list":             {"proton calendar settings list"},
	"proton calendar settings set": {
		"proton calendar settings set week-start monday",
		"proton calendar settings set default-duration 30",
	},

	// ── contacts ──
	"proton contacts list": {
		"proton contacts list",
		"proton contacts list --output json",
	},
	"proton contacts export": {
		"proton contacts export --dest-dir ./address-book",
		"proton contacts export jane --dest jane.vcf",
		"proton contacts export --dest - > contacts.vcf",
	},
	"proton contacts import": {
		"proton contacts import contacts.vcf",
		"proton contacts import contacts.vcf --dry-run",
		"proton contacts import google.vcf --no-groups",
		"proton contacts import - < exported.vcf",
	},
	"proton contacts merge": {
		"proton contacts merge --dry-run",
		"proton contacts merge",
	},
	"proton contacts get": {
		"proton contacts get jane@example.com",
		"proton contacts get 'Jane Roe'",
	},
	"proton contacts create": {
		"proton contacts create --name 'Jane Roe' --email jane@example.com",
		"proton contacts create --name 'Jane Roe' --email work:jane@acme.com --phone cell:+43123456 --anniversary 2015-06-20",
		"proton contacts create --name 'Jane Roe' --email jane@example.com --phone '+43 660 1234567' --organization Acme",
	},
	"proton contacts update": {
		"proton contacts update jane --job-title 'Head of Design'",
		"proton contacts update jane --email jane.roe@work.example --birthday 1990-04-16",
	},
	"proton contacts delete": {"proton contacts delete jane"},
	"proton contacts groups get": {
		"proton contacts groups get Team",
	},
	"proton contacts groups list": {"proton contacts groups list"},
	"proton contacts groups create": {
		"proton contacts groups create --name Team",
		"proton contacts groups create --name Family --color strawberry",
	},
	"proton contacts groups update": {
		"proton contacts groups update Team --name Engineering",
		"proton contacts groups update Team --color reef",
	},
	"proton contacts groups delete": {"proton contacts groups delete Team"},
	"proton contacts groups add":    {"proton contacts groups add Team jane"},
	"proton contacts groups remove": {"proton contacts groups remove Team jane"},
	"proton contacts keys list":     {"proton contacts keys list jane"},
	"proton contacts keys pin": {
		"proton contacts keys pin jane --key jane-pubkey.asc",
		"proton contacts keys pin jane --email jane@example.com --key - --no-encrypt",
	},
	"proton contacts keys unpin": {
		"proton contacts keys unpin jane",
		"proton contacts keys unpin jane --email jane@example.com",
	},

	// ── drive ──
	"proton drive items list": {
		"proton drive items list",
		"proton drive items list /Documents",
	},
	"proton drive items get": {"proton drive items get /Documents/report.pdf"},
	"proton drive items upload": {
		"proton drive items upload ./report.pdf /Documents",
		"proton drive items upload --recursive ./project /Backup",
		"proton drive items upload --if-exists replace ./report.pdf /Documents",
		"pg_dump mydb | gzip | proton drive items upload - /Backups/db.sql.gz",
	},
	"proton drive items download": {
		"proton drive items download /Documents/report.pdf --dest-dir .",
		"proton drive items download /Documents/report.pdf --dest - > report.pdf",
	},
	"proton drive items update": {"proton drive items update /Documents/report.pdf --name summary.pdf"},
	"proton drive items move": {
		"proton drive items move /Documents/report.pdf --into /Archive",
		"proton drive items move --pattern '*.log' --scope /Build --recursive --into /Archive",
	},
	"proton drive items copy": {
		"proton drive items copy /Documents/report.pdf --into /Archive",
		"proton drive items copy --pattern '*.pdf' --scope /Documents --into /Backup",
	},
	"proton drive items trash": {
		"proton drive items trash /Documents/report.pdf",
		"proton drive items trash --pattern '*.tmp' --scope /Build --recursive",
		"proton drive items trash --older-than 1y --scope /Downloads --dry-run",
	},
	"proton drive items delete": {
		"proton drive items delete /Documents/report.pdf",
		"proton drive items delete --pattern '*.tmp' --scope /Build --recursive --yes",
	},
	"proton drive items revisions list":     {"proton drive items revisions list /Documents/report.pdf"},
	"proton drive items revisions restore":  {"proton drive items revisions restore /Documents/report.pdf 5bH2mQxK"},
	"proton drive items revisions download": {"proton drive items revisions download /Documents/report.pdf 5bH2mQxK --dest-dir ."},
	"proton drive items revisions delete":   {"proton drive items revisions delete /Documents/report.pdf 5bH2mQxK"},
	"proton drive items create": {
		"proton drive items create /Documents/2026",
	},
	"proton drive items share get": {"proton drive items share get /Documents/report.pdf"},
	"proton drive items share link": {
		"proton drive items share link /Documents/report.pdf",
		"proton drive items share link /Documents/report.pdf --expires 7d --link-password-file /run/secrets/report-link",
		"proton drive items share link /Documents/report.pdf --clear-link-password --expires never",
		"proton drive items share link /Documents --edit",
	},
	"proton drive items share unlink": {"proton drive items share unlink /Documents/report.pdf"},
	"proton drive items share add": {
		"proton drive items share add /Documents jane@example.com",
		"proton drive items share add /Documents jane@example.com --edit --message 'Have a look'",
	},
	"proton drive items share remove": {"proton drive items share remove /Documents jane@example.com"},
	"proton drive invitations list":   {"proton drive invitations list"},
	"proton drive invitations accept": {
		"proton drive invitations accept 5bH2mQxK",
	},
	"proton drive invitations decline": {"proton drive invitations decline 5bH2mQxK"},
	"proton drive items share update": {
		"proton drive items share update /Reports jane@proton.me --edit",
		"proton drive items share update /Reports jane@proton.me --edit=false",
	},
	"proton drive items share resend": {
		"proton drive items share resend /Reports jane@proton.me",
	},
	"proton drive shared list":  {"proton drive shared list"},
	"proton drive sharing list": {"proton drive sharing list"},
	"proton drive trash list": {
		"proton drive trash list",
		"proton drive trash list --sort trashed --desc",
	},
	"proton drive trash restore": {"proton drive trash restore 5bH2mQxK"},
	"proton drive trash empty":   {"proton drive trash empty"},
	"proton drive photos list": {
		"proton drive photos list",
		"proton drive photos list --album Holidays",
		"proton drive photos list --tag favorites",
	},
	"proton drive photos upload":      {"proton drive photos upload ./IMG_2291.jpg"},
	"proton drive photos download":    {"proton drive photos download 5bH2mQxK --dest-dir ."},
	"proton drive photos favorite":    {"proton drive photos favorite 5bH2mQxK"},
	"proton drive photos unfavorite":  {"proton drive photos unfavorite 5bH2mQxK"},
	"proton drive photos trash":       {"proton drive photos trash 5bH2mQxK"},
	"proton drive photos delete":      {"proton drive photos delete 5bH2mQxK"},
	"proton drive photos albums list": {"proton drive photos albums list"},
	"proton drive photos albums update": {
		"proton drive photos albums update Holidays --cover 5bH2mQxK",
	},
	"proton drive photos albums create": {
		"proton drive photos albums create --name Holidays",
	},
	"proton drive photos albums add":    {"proton drive photos albums add Holidays 5bH2mQxK"},
	"proton drive photos albums remove": {"proton drive photos albums remove Holidays 5bH2mQxK"},
	"proton drive photos albums delete": {
		"proton drive photos albums delete Holidays",
		"proton drive photos albums delete Holidays --delete-photos",
	},
	"proton drive settings get":  {"proton drive settings get"},
	"proton drive settings list": {"proton drive settings list"},
	"proton drive settings set":  {"proton drive settings set revision-retention 30"},

	// ── mail: messages ──
	"proton mail messages list": {
		"proton mail messages list",
		"proton mail messages list --unread",
		"proton mail messages list --folder archive --page-size 50",
		"proton mail messages list --starred --output json",
		"proton mail messages list --from billing@example.com --folder all",
		"proton mail messages list --keyword invoice --after 2026-01-01 --folder all",
	},
	"proton mail messages watch": {
		"proton mail messages watch",
		"proton mail messages watch --folder all",
		"proton mail messages watch --from billing@example.com",
		"proton mail messages watch --output json",
	},
	"proton mail messages get": {
		"proton mail messages get 'Invoice #2291'",
		"proton mail messages get 5bH2mQxK --render html",
		"proton mail messages get 5bH2mQxK --body-only --strip-quotes",
	},
	"proton mail messages send": {
		"proton mail messages send --to jane@example.com --subject Report --body 'See attached.' --attach ./report.pdf",
		"proton mail messages send --to team@example.com --subject Standup --body -",
		"proton mail messages send --to jane@example.com --subject Reminder --send-at 2026-04-16T09:00",
		"proton mail messages send --to jane@example.com --subject Invoice --body 'See attached.' --eo-password-file /run/secrets/jane",
		"proton mail messages send --eml ./draft.eml",
	},
	"proton mail messages reply": {
		"proton mail messages reply 'Invoice #2291' --body 'Thanks, paid today.'",
		"proton mail messages reply 'Invoice #2291' --everyone --body 'Noted.'",
		"proton mail messages reply 'Invoice #2291' --body 'Draft first.' --draft",
	},
	"proton mail messages forward": {
		"proton mail messages forward 'Invoice #2291' --to jane@example.com",
		"proton mail messages forward 'Invoice #2291' --to jane@example.com --no-attachments",
	},
	"proton mail messages move": {
		"proton mail messages move 'Invoice #2291' --into archive",
		"proton mail messages move --from newsletter@example.com --older-than 90d --into archive",
	},
	"proton mail messages trash": {
		"proton mail messages trash 'Invoice #2291'",
		"proton mail messages trash --unread --older-than 30d",
		"proton mail messages trash --from newsletter@example.com --older-than 90d --dry-run",
	},
	"proton mail messages delete": {
		"proton mail messages delete 5bH2mQxK",
		"proton mail messages delete --folder spam --all --yes",
	},
	"proton mail messages label": {
		"proton mail messages label 'Invoice #2291' --label Accounting",
		"proton mail messages label --from billing@example.com --label Accounting",
	},
	"proton mail messages unlabel": {"proton mail messages unlabel 'Invoice #2291' --label Accounting"},
	"proton mail messages star":    {"proton mail messages star 'Invoice #2291'"},
	"proton mail messages unstar":  {"proton mail messages unstar 'Invoice #2291'"},
	"proton mail messages mark read": {
		"proton mail messages mark read 'Invoice #2291'",
		"proton mail messages mark read --folder inbox --all",
	},
	"proton mail messages mark unread": {"proton mail messages mark unread 'Invoice #2291'"},
	"proton mail messages unschedule": {
		"proton mail messages unschedule 5bH2mQxK",
		"proton mail messages unschedule --all",
	},
	"proton mail messages empty": {
		"proton mail messages empty --folder trash",
		"proton mail messages empty --folder spam",
	},
	"proton mail messages expire": {
		"proton mail messages expire 5bH2mQxK --in 7d",
		"proton mail messages expire --from newsletter@example.com --in 30d",
		"proton mail messages expire 5bH2mQxK --never",
	},
	"proton mail messages unsubscribe": {
		"proton mail messages unsubscribe 5bH2mQxK",
	},
	"proton mail messages export": {
		"proton mail messages export 'Invoice #2291' --dest-dir ./backup",
		"proton mail messages export --folder archive --all --dest-dir ./mail-backup",
		"proton mail messages export --folder archive --older-than 1y --format mbox --dest archive.mbox",
	},
	"proton mail messages attachments list": {
		"proton mail messages attachments list 'Invoice #2291'",
		"proton mail messages attachments list 5bH2mQxK --include-inline",
	},
	"proton mail messages attachments download": {
		"proton mail messages attachments download 'Invoice #2291' --dest-dir .",
		"proton mail messages attachments download 5bH2mQxK kQ81mDx4 --dest invoice.pdf",
	},

	// ── mail: conversations ──
	"proton mail conversations list": {
		"proton mail conversations list",
		"proton mail conversations list --unread --folder inbox",
		"proton mail conversations list --from jane@example.com --folder all",
	},
	"proton mail conversations get": {
		"proton mail conversations get 'Quarterly numbers'",
		"proton mail conversations get 5bH2mQxK --summary",
	},
	"proton mail conversations snooze": {
		"proton mail conversations snooze 5bH2mQxK --until 3d",
		"proton mail conversations snooze --unread --until 2026-04-17T09:00",
	},
	"proton mail conversations unsnooze": {
		"proton mail conversations unsnooze 5bH2mQxK",
	},
	"proton mail conversations reply": {
		"proton mail conversations reply 'Quarterly numbers' --body 'Looks right to me.'",
		"proton mail conversations reply 'Quarterly numbers' --everyone --body Agreed.",
	},
	"proton mail conversations forward": {
		"proton mail conversations forward 'Quarterly numbers' --to jane@example.com",
	},
	"proton mail conversations move": {
		"proton mail conversations move 'Quarterly numbers' --into archive",
		"proton mail conversations move --older-than 90d --folder inbox --into archive",
	},
	"proton mail conversations trash": {
		"proton mail conversations trash 'Quarterly numbers'",
		"proton mail conversations trash --from newsletter@example.com --older-than 90d",
	},
	"proton mail conversations delete": {"proton mail conversations delete 5bH2mQxK"},
	"proton mail conversations label": {
		"proton mail conversations label 'Quarterly numbers' --label Accounting",
	},
	"proton mail conversations unlabel": {
		"proton mail conversations unlabel 'Quarterly numbers' --label Accounting",
	},
	"proton mail conversations star":   {"proton mail conversations star 'Quarterly numbers'"},
	"proton mail conversations unstar": {"proton mail conversations unstar 'Quarterly numbers'"},
	"proton mail conversations mark read": {
		"proton mail conversations mark read 'Quarterly numbers'",
		"proton mail conversations mark read --folder inbox --all",
	},
	"proton mail conversations mark unread": {"proton mail conversations mark unread 'Quarterly numbers'"},
	"proton mail conversations export": {
		"proton mail conversations export 'Quarterly numbers' --dest-dir ./backup",
	},
	"proton mail conversations attachments list": {
		"proton mail conversations attachments list 'Quarterly numbers'",
	},
	"proton mail conversations attachments download": {
		"proton mail conversations attachments download 'Quarterly numbers' --dest-dir .",
	},

	// ── mail: drafts ──
	"proton mail drafts list": {"proton mail drafts list"},
	"proton mail drafts create": {
		"proton mail drafts create --to team@example.com --subject Standup --body 'Notes to follow.'",
		"proton mail drafts create --to jane@example.com --subject Report --attach ./report.pdf",
	},
	"proton mail drafts update": {
		"proton mail drafts update 5bH2mQxK --body 'Notes attached.'",
		"proton mail drafts update 5bH2mQxK --detach report.pdf",
	},
	"proton mail drafts send": {
		"proton mail drafts send 5bH2mQxK",
		"proton mail drafts send 5bH2mQxK --send-at 2026-04-16T09:00",
	},
	"proton mail drafts delete": {"proton mail drafts delete 5bH2mQxK"},

	// ── mail: settings ──
	"proton mail settings get":  {"proton mail settings get"},
	"proton mail settings list": {"proton mail settings list"},
	"proton mail settings set": {
		"proton mail settings set signature off",
		"proton mail settings set view-mode conversation",
	},
	"proton mail settings labels list": {"proton mail settings labels list"},
	"proton mail settings labels create": {
		"proton mail settings labels create --name Work",
		"proton mail settings labels create --name Accounting --color pacific",
	},
	"proton mail settings labels update": {
		"proton mail settings labels update Work --name Office",
		"proton mail settings labels update Work --color enzian",
	},
	"proton mail settings labels delete": {"proton mail settings labels delete Work"},
	"proton mail settings folders list":  {"proton mail settings folders list"},
	"proton mail settings folders create": {
		"proton mail settings folders create --name Receipts",
		"proton mail settings folders create --name 2026 --parent Receipts --color olive",
		"proton mail settings folders create --name Receipts --notify=false",
	},
	"proton mail settings folders update": {
		"proton mail settings folders update Receipts --name Invoices",
		"proton mail settings folders update Receipts --notify",
	},
	"proton mail settings folders delete": {"proton mail settings folders delete Receipts"},
	"proton mail settings addresses list": {"proton mail settings addresses list"},
	"proton mail settings addresses get":  {"proton mail settings addresses get me@proton.me"},
	"proton mail settings addresses update": {
		"proton mail settings addresses update me@proton.me --display-name 'Roman'",
		"proton mail settings addresses update me@proton.me --signature - --html",
		"proton mail settings addresses update me@proton.me --clear-signature",
	},
	"proton mail settings filters apply": {
		"proton mail settings filters apply",
		"proton mail settings filters apply Newsletters",
	},
	"proton mail settings filters reorder": {
		"proton mail settings filters reorder Newsletters Receipts Archive",
	},
	"proton mail settings senders list": {"proton mail settings senders list"},
	"proton mail settings senders block": {
		"proton mail settings senders block spammer@example.com",
		"proton mail settings senders block @example.com",
	},
	"proton mail settings senders spam": {
		"proton mail settings senders spam newsletter@example.com",
	},
	"proton mail settings senders allow": {
		"proton mail settings senders allow billing@example.com",
	},
	"proton mail settings senders forget": {
		"proton mail settings senders forget billing@example.com",
	},
	"proton mail settings filters list": {"proton mail settings filters list"},
	"proton mail settings filters get": {
		"proton mail settings filters get Receipts",
	},
	"proton mail settings filters create": {
		`proton mail settings filters create --name Receipts --if "sender contains billing@" --label Receipts`,
		"proton mail settings filters create --name Big --sieve ./big.sieve",
	},
	"proton mail settings filters update":  {"proton mail settings filters update Receipts --sieve ./receipts.sieve"},
	"proton mail settings filters enable":  {"proton mail settings filters enable Receipts"},
	"proton mail settings filters disable": {"proton mail settings filters disable Receipts"},
	"proton mail settings filters delete":  {"proton mail settings filters delete Receipts"},
	"proton mail settings autoreply get":   {"proton mail settings autoreply get"},
	"proton mail settings autoreply set": {
		"proton mail settings autoreply set --repeat permanent --message 'Away until Monday.'",
		"proton mail settings autoreply set --message 'On holiday.' --start 2026-07-01T09:00 --end 2026-07-14T17:00",
	},
	"proton mail settings autoreply enable":   {"proton mail settings autoreply enable"},
	"proton mail settings autoreply disable":  {"proton mail settings autoreply disable"},
	"proton mail settings forwarding list":    {"proton mail settings forwarding list"},
	"proton mail settings forwarding get":     {"proton mail settings forwarding get jane@proton.me"},
	"proton mail settings forwarding create":  {"proton mail settings forwarding create me@proton.me jane@proton.me"},
	"proton mail settings forwarding enable":  {"proton mail settings forwarding enable jane@proton.me"},
	"proton mail settings forwarding disable": {"proton mail settings forwarding disable jane@proton.me"},
	"proton mail settings forwarding resend":  {"proton mail settings forwarding resend jane@proton.me"},
	"proton mail settings forwarding delete":  {"proton mail settings forwarding delete jane@proton.me"},

	// ── pass ──
	"proton pass vaults share add": {
		"proton pass vaults share add Work jane@proton.me",
		"proton pass vaults share add Work jane@proton.me --access editor",
	},
	"proton pass vaults share get": {
		"proton pass vaults share get Work",
	},
	"proton pass vaults share update": {
		"proton pass vaults share update Work jane@proton.me --access manager",
	},
	"proton pass vaults share remove": {
		"proton pass vaults share remove Work jane@proton.me",
	},
	"proton pass vaults transfer": {
		"proton pass vaults transfer Work jane@proton.me",
	},
	"proton pass items share add": {
		"proton pass items share add github.com jane@proton.me",
		"proton pass items share add github.com jane@proton.me --access editor",
	},
	"proton pass items share get": {
		"proton pass items share get github.com",
	},
	"proton pass items share update": {
		"proton pass items share update github.com jane@proton.me --access viewer",
	},
	"proton pass items share remove": {
		"proton pass items share remove github.com jane@proton.me",
	},
	"proton pass shared list":  {"proton pass shared list"},
	"proton pass sharing list": {"proton pass sharing list"},
	"proton pass invitations list": {
		"proton pass invitations list",
	},
	"proton pass invitations accept": {
		"proton pass invitations accept Work",
	},
	"proton pass invitations decline": {
		"proton pass invitations decline Work",
	},
	"proton pass aliases contacts list": {
		"proton pass aliases contacts list shopping",
	},
	"proton pass aliases contacts create": {
		"proton pass aliases contacts create shopping seller@example.com",
		`proton pass aliases contacts create shopping seller@example.com --name "The seller"`,
	},
	"proton pass aliases contacts delete": {
		"proton pass aliases contacts delete shopping seller@example.com",
	},
	"proton pass aliases contacts block": {
		"proton pass aliases contacts block shopping seller@example.com",
	},
	"proton pass aliases contacts allow": {
		"proton pass aliases contacts allow shopping seller@example.com",
	},
	"proton pass settings mailboxes create": {
		"proton pass settings mailboxes create me@example.com",
	},
	"proton pass settings mailboxes verify": {
		"proton pass settings mailboxes verify me@example.com --code 123456",
	},
	"proton pass settings mailboxes resend": {
		"proton pass settings mailboxes resend me@example.com",
	},
	"proton pass settings mailboxes update": {
		"proton pass settings mailboxes update me@example.com --default",
	},
	"proton pass settings mailboxes delete": {
		"proton pass settings mailboxes delete me@example.com --transfer-to other@example.com",
	},
	"proton pass settings domains list": {
		"proton pass settings domains list",
	},
	"proton pass settings mailboxes list": {
		"proton pass settings mailboxes list",
	},
	"proton pass links list": {
		"proton pass links list",
	},
	"proton pass links get": {
		"proton pass links get 5bH2mQxK",
	},
	"proton pass links create": {
		"proton pass links create github.com --expires 7d",
		"proton pass links create github.com --expires 24h --views 1",
	},
	"proton pass links revoke": {
		"proton pass links revoke 5bH2mQxK",
	},
	"proton pass breaches list": {
		"proton pass breaches list",
	},
	"proton pass breaches get": {
		"proton pass breaches get jane@proton.me",
	},
	"proton pass export": {
		"proton pass export --dest pass-backup.zip --passphrase-file ~/.backup-passphrase",
		"proton pass export --dest pass-backup.zip",
	},
	"proton pass import": {
		"proton pass import pass-backup.zip --passphrase-file ~/.backup-passphrase",
		"proton pass import --dry-run pass-backup.zip",
	},
	"proton pass generate": {
		"proton pass generate",
		"proton pass generate --length 32",
		"proton pass generate --no-symbols --length 24",
		"proton pass generate --words 4",
		"proton pass generate --words 4 --separator space --no-digits",
	},
	"proton pass items list": {
		"proton pass items list",
		"proton pass items list --vault Work",
		"proton pass items list --type login",
	},
	"proton pass items get": {
		"proton pass items get github.com",
		"proton pass items get GitHub --output json",
	},
	"proton pass items create": {
		"proton pass items create --name GitHub --username roman --url github.com --generate-password",
		"proton pass items create --name Router --generate-password --words 5",
		"proton pass items create --type note --name 'Door codes' --note 'Front: 1234'",
		"proton pass items create --type credit-card --name 'Travel card' --holder 'Roman' --expiry 2030-04 --secret-file number=/run/secrets/card",
		"proton pass items create --type custom --name Router --field 'Network/SSID=home' --secret-file 'Network/Key=/run/secrets/wifi'",
	},
	"proton pass items update": {
		"proton pass items update GitHub --secret-file password=/run/secrets/github",
		"proton pass items update GitHub --secret-stdin password",
		"proton pass items update GitHub --username roman-16 --url github.com",
		"proton pass items update GitHub --generate-password",
	},
	"proton pass items move": {
		"proton pass items move github.com --into Work",
	},
	"proton pass items revisions list": {
		"proton pass items revisions list github.com",
		"proton pass items revisions list github.com --output json",
	},
	"proton pass items revisions get": {
		"proton pass items revisions get github.com 3",
	},
	"proton pass items totp": {
		"proton pass items totp github.com",
		"proton pass items totp github.com --output json",
	},
	"proton pass items pin": {
		"proton pass items pin github.com",
	},
	"proton pass items unpin": {
		"proton pass items unpin github.com",
	},
	"proton pass items trash": {
		"proton pass items trash GitHub",
		"proton pass items trash --vault Work --older-than 1y",
	},
	"proton pass items delete": {
		"proton pass items delete GitHub",
		"proton pass items delete --vault Work --all --yes",
	},
	"proton pass vaults list": {"proton pass vaults list"},
	"proton pass vaults get":  {"proton pass vaults get Work"},
	"proton pass vaults create": {
		"proton pass vaults create --name Work",
	},
	"proton pass vaults update": {
		"proton pass vaults update Work --name Office",
		"proton pass vaults update Work --description 'Shared team logins' --icon 7 --color 3",
	},
	"proton pass vaults delete":   {"proton pass vaults delete Work"},
	"proton pass aliases list":    {"proton pass aliases list", "proton pass aliases list --vault Work"},
	"proton pass aliases options": {"proton pass aliases options"},
	"proton pass aliases create": {
		"proton pass aliases create --prefix shop --mailbox me@proton.me",
		"proton pass aliases create --prefix news --mailbox me@proton.me --vault Work --name 'Newsletter alias'",
	},
	"proton pass aliases enable":  {"proton pass aliases enable shop"},
	"proton pass aliases disable": {"proton pass aliases disable shop"},
	"proton pass trash list":      {"proton pass trash list"},
	"proton pass trash restore": {
		"proton pass trash restore GitHub",
		"proton pass trash restore --all",
	},
	"proton pass trash empty": {"proton pass trash empty"},

	// ── proton itself ──
	"proton changelog": {
		"proton changelog",
		"proton changelog 2.4.1",
		"proton changelog --since 2.3.0",
		"proton changelog --since 2.3.0 --until 2.4.0",
	},
	"proton report": {
		"proton report",
		"proton report --all",
		"proton report --dest bug.txt",
	},
	"proton skill": {
		"proton skill",
		"proton skill --body-only",
		"proton skill > skills/proton-cli/SKILL.md",
	},
	"proton version": {
		"proton version",
		"proton version --output json",
	},
	"proton update": {
		"proton update",
		"proton update --check",
		"proton update 1.9.11",
		"proton update --reinstall",
	},
	"proton uninstall": {
		"proton uninstall --dry-run",
		"proton uninstall --yes",
		"proton uninstall --yes --purge",
	},
}

// attachExamples gives every leaf the examples filed under its path.
//
// Cobra indents an Example block itself only in some templates, so the lines are
// indented here, once, and every command's help reads the same.
func attachExamples(root *cobra.Command) {
	var walk func(*cobra.Command)
	walk = func(c *cobra.Command) {
		// Stored as the command lines they are. Whoever shows them decides how
		// they are laid out: a help screen indents them, a reference page fences
		// them, and neither wants the other's whitespace baked in.
		if lines, ok := examples[c.CommandPath()]; ok {
			c.Example = strings.Join(lines, "\n")
		}
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(root)
}
