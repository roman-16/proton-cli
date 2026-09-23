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
	"proton account keys reactivate": {
		"proton account keys reactivate",
		"proton account keys reactivate --recovery-phrase",
		"proton account keys reactivate --recovery-file ~/Downloads/proton_recovery.asc",
		"proton account keys reactivate --previous-password-file /run/secrets/proton-old --password-file /run/secrets/proton",
	},
	"proton account login": {
		"proton account login",
		"proton account login --profile work",
		"proton account login --user me@proton.me --password-file /run/secrets/proton",
		"proton account login --user me@proton.me --password-file - --totp 123456",
		"proton account login --user me@proton.me --password-file /run/secrets/proton --second-password-file /run/secrets/proton-second",
		"proton account login --user me@proton.me --password-file /run/secrets/proton --extra-password-file /run/secrets/proton-pass",
		"proton account login --qr",
	},
	"proton account logout": {
		"proton account logout",
		"proton account logout --revoke",
		"proton account logout --all",
	},
	"proton account profiles list":   {"proton account profiles list"},
	"proton account profiles delete": {"proton account profiles delete work"},
	"proton account security-log list": {
		"proton account security-log list",
		"proton account security-log list --limit 0 --output json",
	},
	"proton account security-log get": {"proton account security-log get"},
	"proton account security-log enable": {
		"proton account security-log enable",
		"proton account security-log enable --detailed",
		"proton account security-log enable --detailed --password-file /run/secrets/proton",
	},
	"proton account security-log disable": {
		"proton account security-log disable --detailed",
		"proton account security-log disable",
	},
	"proton account security-log delete": {
		"proton account security-log delete",
		"proton account security-log delete --yes --password-file /run/secrets/proton",
	},
	"proton account sessions create": {
		"proton account sessions create 0:8FJ3K2QP:qm4Xb2v9tR1sLp0yZcHwKdNfEuAgJi7MoBxVn3Tl5Qs=:Other",
	},
	"proton account sessions list": {"proton account sessions list"},
	"proton account sessions revoke": {
		"proton account sessions revoke 5bH2mQxK",
		"proton account sessions revoke --others",
		"proton account sessions revoke 5bH2mQxK --password-file /run/secrets/proton",
	},
	"proton account settings get":  {"proton account settings get"},
	"proton account settings list": {"proton account settings list"},
	"proton account settings set": {
		"proton account settings set locale de_AT",
		"proton account settings set week-start monday",
	},
	"proton account settings emergency-access list": {
		"proton account settings emergency-access list",
		"proton account settings emergency-access list --incoming",
	},
	"proton account settings emergency-access get": {"proton account settings emergency-access get 7Kd2p1Qa"},
	"proton account settings emergency-access add": {
		"proton account settings emergency-access add jane.roe@proton.me",
		"proton account settings emergency-access add jane.roe@proton.me --wait 3d --password-file /run/secrets/proton",
	},
	"proton account settings emergency-access update": {
		"proton account settings emergency-access update 7Kd2p1Qa --wait 3d --password-file /run/secrets/proton",
	},
	"proton account settings emergency-access request": {"proton account settings emergency-access request 4Gh2k9Lp"},
	"proton account settings emergency-access access":  {"proton account settings emergency-access access 4Gh2k9Lp --as dads-account"},
	"proton account settings emergency-access grant":   {"proton account settings emergency-access grant 7Kd2p1Qa --password-file /run/secrets/proton"},
	"proton account settings emergency-access cancel":  {"proton account settings emergency-access cancel 7Kd2p1Qa"},
	"proton account settings emergency-access remove":  {"proton account settings emergency-access remove 7Kd2p1Qa"},
	"proton account settings password set": {
		"proton account settings password set",
		"proton account settings password set --password-file /run/secrets/proton --new-password-file /run/secrets/proton-new",
	},
	"proton account settings recovery-contacts list": {
		"proton account settings recovery-contacts list",
		"proton account settings recovery-contacts list --incoming",
	},
	"proton account settings recovery-contacts get": {"proton account settings recovery-contacts get 3Np7xQ2b"},
	"proton account settings recovery-contacts add": {
		"proton account settings recovery-contacts add alex.roe@proton.me",
		"proton account settings recovery-contacts add alex.roe@proton.me --password-file /run/secrets/proton",
	},
	"proton account settings recovery-contacts remove": {"proton account settings recovery-contacts remove 3Np7xQ2b"},
	"proton account settings recovery-email get":       {"proton account settings recovery-email get"},
	"proton account settings recovery-email set": {
		"proton account settings recovery-email set jane.roe@example.com",
		"proton account settings recovery-email set none",
	},
	"proton account settings recovery-email verify":  {"proton account settings recovery-email verify"},
	"proton account settings recovery-email enable":  {"proton account settings recovery-email enable"},
	"proton account settings recovery-email disable": {"proton account settings recovery-email disable"},
	"proton account settings recovery-phone get":     {"proton account settings recovery-phone get"},
	"proton account settings recovery-phone set": {
		"proton account settings recovery-phone set '+43 660 1234567'",
		"proton account settings recovery-phone set none",
	},
	"proton account settings recovery-phone verify": {
		"proton account settings recovery-phone verify",
		"proton account settings recovery-phone verify --code 482913",
	},
	"proton account settings recovery-phone enable":  {"proton account settings recovery-phone enable"},
	"proton account settings recovery-phone disable": {"proton account settings recovery-phone disable"},
	"proton account settings recovery-phrase get":    {"proton account settings recovery-phrase get"},
	"proton account settings recovery-phrase set": {
		"proton account settings recovery-phrase set",
		"proton account settings recovery-phrase set --output json",
	},
	"proton account settings recovery-phrase disable": {"proton account settings recovery-phrase disable"},
	"proton account settings second-password get":     {"proton account settings second-password get"},
	"proton account settings second-password enable": {
		"proton account settings second-password enable",
		"proton account settings second-password enable --password-file /run/secrets/proton --new-password-file /run/secrets/proton-second",
	},
	"proton account settings second-password set": {
		"proton account settings second-password set",
		"proton account settings second-password set --password-file /run/secrets/proton --new-password-file /run/secrets/proton-second",
	},
	"proton account settings second-password disable": {"proton account settings second-password disable"},
	"proton account settings security-keys list":      {"proton account settings security-keys list"},
	"proton account settings security-keys create": {
		`proton account settings security-keys create --name "YubiKey 5C"`,
		`proton account settings security-keys create --name "Spare key" --password-file /run/secrets/proton`,
	},
	"proton account settings security-keys update": {
		`proton account settings security-keys update "Spare key" --name "Key in the safe"`,
	},
	"proton account settings security-keys delete": {
		`proton account settings security-keys delete "Key in the safe"`,
		`proton account settings security-keys delete 5bH2mQxK --yes --password-file /run/secrets/proton`,
	},
	"proton account settings two-factor get": {"proton account settings two-factor get"},
	"proton account settings two-factor generate": {
		"proton account settings two-factor generate",
		"proton account settings two-factor generate --output json",
	},
	"proton account settings two-factor enable": {
		"proton account settings two-factor enable",
		"proton account settings two-factor enable --totp 123456 --password-file /run/secrets/proton",
	},
	"proton account settings two-factor disable": {
		"proton account settings two-factor disable",
		"proton account settings two-factor disable --totp 123456 --password-file /run/secrets/proton",
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
		"proton calendar events export --after 2026-01-01 --before 2026-12-31 --dest year.ics",
		"proton calendar events export --calendar Work --dest - > work.ics",
	},
	"proton calendar events import": {
		"proton calendar events import holidays.ics",
		"proton calendar events import --calendar Work team.ics",
		"curl -s https://example.com/team.ics | proton calendar events import -",
	},
	"proton calendar events list": {
		"proton calendar events list",
		"proton calendar events list --after 2026-04-15 --before 2026-04-30",
		"proton calendar events list --calendar Work",
		"proton calendar events list --keyword dentist",
	},
	"proton calendar reminders list": {
		"proton calendar reminders list",
		"proton calendar reminders list --after 2026-04-20 --before 2026-04-21",
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
		"proton calendar settings calendars share add Work jane@proton.me --access editor",
	},
	"proton calendar settings calendars share get": {
		"proton calendar settings calendars share get Work",
	},
	"proton calendar settings calendars share update": {
		"proton calendar settings calendars share update Work jane@proton.me --access viewer",
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
	"proton calendar settings links create": {
		"proton calendar settings links create Work",
		"proton calendar settings links create Work --access full --name 'Team feed'",
	},
	"proton calendar settings links get": {
		"proton calendar settings links get 'Team feed'",
	},
	"proton calendar settings links list": {
		"proton calendar settings links list",
		"proton calendar settings links list --calendar Work",
	},
	"proton calendar settings links update": {
		"proton calendar settings links update 'Team feed' --name 'Team, read-only'",
		"proton calendar settings links update 'Team feed' --clear-name",
	},
	"proton calendar settings links revoke": {
		"proton calendar settings links revoke 'Team feed'",
	},
	"proton calendar settings get":  {"proton calendar settings get"},
	"proton calendar settings list": {"proton calendar settings list"},
	"proton calendar settings set": {
		"proton calendar settings set view week",
		"proton calendar settings set primary-timezone Europe/Vienna",
	},

	// ── the local index ──
	"proton index list": {
		"proton index list",
		"proton index list --output json",
	},
	"proton index create": {
		"proton index create",
		"proton index create mail",
		"proton index create drive calendar",
		"proton index create drive --dry-run",
	},
	"proton index update": {
		"proton index update",
		"proton index update --quiet",
	},
	"proton index watch": {
		"proton index watch",
		"proton index watch --output json",
	},
	"proton index delete": {
		"proton index delete",
		"proton index delete --yes mail",
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
		"proton contacts keys pin jane@example.com --key - --no-encrypt",
	},
	"proton contacts keys unpin": {
		"proton contacts keys unpin jane",
		"proton contacts keys unpin jane@example.com",
	},

	// ── drive ──
	"proton drive computers list": {"proton drive computers list"},
	"proton drive computers update": {
		"proton drive computers update 'Work laptop' --name 'Office PC'",
	},
	"proton drive computers delete": {"proton drive computers delete 7Kd91mQx"},
	"proton drive items list": {
		"proton drive items list",
		"proton drive items list /Documents",
		"proton drive items list /Notes --keyword 'parking permit' --recursive",
		"proton drive items list / --computer 'Work laptop'",
		"proton drive items list / --shared Project",
		"proton drive items list / --link 'https://drive.proton.me/urls/7X2K9M3N1P#kQ81mDx4T9wL'",
	},
	"proton drive items get": {
		"proton drive items get /Documents/report.pdf",
		"proton drive items get / --shared Q3-report.pdf",
	},
	"proton drive items upload": {
		"proton drive items upload ./report.pdf /Documents",
		"proton drive items upload --recursive ./project /Backup",
		"proton drive items upload --if-exists replace ./report.pdf /Documents",
		"proton drive items upload ./photo.jpg / --link 'https://drive.proton.me/urls/7X2K9M3N1P#kQ81mDx4T9wL'",
		"pg_dump mydb | gzip | proton drive items upload - /Backups/db.sql.gz",
	},
	"proton drive items download": {
		"proton drive items download /Documents/report.pdf --dest-dir .",
		"proton drive items download /Documents --recursive --dest-dir .",
		"proton drive items download /Documents/report.pdf --dest - > report.pdf",
		"proton drive items download /report.pdf --shared Project --dest-dir .",
		"proton drive items download / --link 'https://drive.proton.me/urls/7X2K9M3N1P#kQ81mDx4T9wL' --dest-dir .",
		"proton drive items download / --link 'https://drive.proton.me/urls/7X2K9M3N1P#kQ81mDx4T9wL' --link-password-file /run/secrets/q3-link --dest-dir .",
	},
	"proton drive items update": {
		"proton drive items update /Documents/report.pdf --name summary.pdf",
		"proton drive items update /photo.jpg --name holiday.jpg --link 'https://drive.proton.me/urls/7X2K9M3N1P#kQ81mDx4T9wL'",
	},
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
		"proton drive items delete /photo.jpg --link 'https://drive.proton.me/urls/7X2K9M3N1P#kQ81mDx4T9wL'",
	},
	"proton drive items revisions list":     {"proton drive items revisions list /Documents/report.pdf"},
	"proton drive items revisions restore":  {"proton drive items revisions restore /Documents/report.pdf 5bH2mQxK"},
	"proton drive items revisions download": {"proton drive items revisions download /Documents/report.pdf 5bH2mQxK --dest-dir ."},
	"proton drive items revisions delete":   {"proton drive items revisions delete /Documents/report.pdf 5bH2mQxK"},
	"proton drive items create": {
		"proton drive items create /Documents/2026",
		"proton drive items create /2026 --link 'https://drive.proton.me/urls/7X2K9M3N1P#kQ81mDx4T9wL'",
	},
	"proton drive items share get": {"proton drive items share get /Documents/report.pdf"},
	"proton drive links create": {
		"proton drive links create /Documents/report.pdf --expires 7d",
		"proton drive links create /Documents/report.pdf --expires 7d --link-password-file /run/secrets/report-link",
		"proton drive links create /Documents/report.pdf --clear-link-password --expires never",
		"proton drive links create /Documents --access editor",
	},
	"proton drive links get":  {"proton drive links get /Documents/report.pdf"},
	"proton drive links list": {"proton drive links list"},
	"proton drive links revoke": {
		"proton drive links revoke /Documents/report.pdf",
	},
	"proton drive items share add": {
		"proton drive items share add /Documents jane@example.com",
		"proton drive items share add /Documents jane@example.com --access editor --message 'Have a look'",
	},
	"proton drive items share confirm": {
		"proton drive items share confirm /Documents jane@example.com",
	},
	"proton drive items share remove": {"proton drive items share remove /Documents jane@example.com"},
	"proton drive invitations list":   {"proton drive invitations list"},
	"proton drive invitations accept": {
		"proton drive invitations accept 5bH2mQxK",
	},
	"proton drive invitations decline": {"proton drive invitations decline 5bH2mQxK"},
	"proton drive items share update": {
		"proton drive items share update /Reports jane@proton.me --access editor",
		"proton drive items share update /Reports jane@proton.me --access viewer",
	},
	"proton drive items share resend": {
		"proton drive items share resend /Reports jane@proton.me",
	},
	"proton drive shared list": {"proton drive shared list"},
	"proton drive shared add": {
		"proton drive shared add 'https://drive.proton.me/urls/7X2K9M3N1P#kQ81mDx4T9wL'",
		"proton drive shared add 'https://drive.proton.me/urls/7X2K9M3N1P#kQ81mDx4T9wL' --link-password-file /run/secrets/q3-link",
	},
	"proton drive shared remove": {"proton drive shared remove Q3-report.pdf"},
	"proton drive shared leave": {
		"proton drive shared leave Project",
		"proton drive shared leave Project --yes",
	},
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
	"proton drive photos albums share add": {
		"proton drive photos albums share add Holidays jane@proton.me",
		"proton drive photos albums share add Holidays jane@proton.me --access editor --message 'Photos from the trip'",
	},
	"proton drive photos albums share get": {"proton drive photos albums share get Holidays"},
	"proton drive photos albums share update": {
		"proton drive photos albums share update Holidays jane@proton.me --access viewer",
	},
	"proton drive photos albums share remove": {
		"proton drive photos albums share remove Holidays jane@proton.me",
	},
	"proton drive photos albums share resend": {
		"proton drive photos albums share resend Holidays jane@proton.me",
	},
	"proton drive photos albums share confirm": {
		"proton drive photos albums share confirm Holidays sam@example.com",
	},
	"proton drive photos share add": {
		"proton drive photos share add 5bH2mQxK jane@proton.me",
		"proton drive photos share add 5bH2mQxK jane@proton.me --access editor",
	},
	"proton drive photos share get": {"proton drive photos share get 5bH2mQxK"},
	"proton drive photos share update": {
		"proton drive photos share update 5bH2mQxK jane@proton.me --access viewer",
	},
	"proton drive photos share remove": {"proton drive photos share remove 5bH2mQxK jane@proton.me"},
	"proton drive photos share resend": {"proton drive photos share resend 5bH2mQxK jane@proton.me"},
	"proton drive photos share confirm": {
		"proton drive photos share confirm 5bH2mQxK sam@example.com",
	},
	"proton drive photos links create": {
		"proton drive photos links create 5bH2mQxK",
		"proton drive photos links create 5bH2mQxK --expires 7d",
		"proton drive photos links create 5bH2mQxK --link-password-file /run/secrets/photo-link",
	},
	"proton drive photos links get":    {"proton drive photos links get 5bH2mQxK"},
	"proton drive photos links revoke": {"proton drive photos links revoke 5bH2mQxK"},
	"proton drive settings get":        {"proton drive settings get"},
	"proton drive settings list":       {"proton drive settings list"},
	"proton drive settings set":        {"proton drive settings set version-history 30d"},
	"proton drive volumes list":        {"proton drive volumes list"},
	"proton drive volumes restore": {
		"proton drive volumes restore 7Kd91mQx",
		"proton drive volumes restore --all",
	},
	"proton drive volumes delete": {
		"proton drive volumes delete 7Kd91mQx",
		"proton drive volumes delete 7Kd91mQx --yes",
	},

	// ── mail: messages ──
	"proton mail messages list": {
		"proton mail messages list",
		"proton mail messages list --unread",
		"proton mail messages list --folder archive --limit 50",
		"proton mail messages list --starred --output json",
		"proton mail messages list --from billing@example.com --folder all",
		"proton mail messages list --keyword invoice --after 2026-01-01 --folder all",
		"proton mail messages list --keyword 'parking permit' --folder all",
		"proton mail messages list --folder all --sort size --limit 10",
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
		"proton mail messages send --to jane@example.com --subject Contract --body 'Signed and attached.' --request-receipt",
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
	"proton mail messages mark legitimate": {
		"proton mail messages mark legitimate 5bH2mQxK",
	},
	"proton mail messages mark phishing": {
		"proton mail messages mark phishing 5bH2mQxK",
		"proton mail messages mark phishing 'Your account will be suspended' --dry-run",
	},
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
	"proton mail messages update": {
		"proton mail messages update 5bH2mQxK --expires 7d",
		"proton mail messages update --from newsletter@example.com --expires 30d",
		"proton mail messages update 5bH2mQxK --expires never",
	},
	"proton mail messages unsubscribe": {
		"proton mail messages unsubscribe 5bH2mQxK",
	},
	"proton mail messages receipt": {
		"proton mail messages receipt 'Invoice #2291'",
		"proton mail messages receipt 5bH2mQxK --dry-run",
	},
	"proton mail mailing-lists list": {
		"proton mail mailing-lists list",
		"proton mail mailing-lists list --sort unread",
		"proton mail mailing-lists list --unsubscribed",
	},
	"proton mail mailing-lists get": {
		"proton mail mailing-lists get 'Trailhead Weekly'",
	},
	"proton mail mailing-lists unsubscribe": {
		"proton mail mailing-lists unsubscribe 'Trailhead Weekly'",
		"proton mail mailing-lists unsubscribe news@example.com --dry-run",
	},
	"proton mail mailing-lists update": {
		"proton mail mailing-lists update 'Trailhead Weekly' --into Archive",
		"proton mail mailing-lists update news@example.com --into Archive --mark-read",
	},
	"proton mail mailing-lists remove": {
		"proton mail mailing-lists remove 'Trailhead Weekly'",
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
		"proton mail messages attachments download 'Invoice #2291' invoice-2291.pdf --dest-dir .",
		"proton mail messages attachments download 5bH2mQxK kQ81mDx4 --dest invoice.pdf",
	},

	// ── mail: conversations ──
	"proton mail conversations list": {
		"proton mail conversations list",
		"proton mail conversations list --unread --folder inbox",
		"proton mail conversations list --from jane@example.com --folder all",
		"proton mail conversations list --sort size --desc",
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
	"proton mail drafts list": {
		"proton mail drafts list",
		"proton mail drafts list --sort size",
	},
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

	// ── mail: protected ──
	"proton mail protected get": {
		"proton mail protected get 9fK2pQ7xNv4mB8 --eo-password-file /run/secrets/jane",
		"proton mail protected get 'https://mail.proton.me/eo/9fK2pQ7xNv4mB8' --eo-password-file -",
		"proton mail protected get 9fK2pQ7xNv4mB8 --eo-password-file /run/secrets/jane --body-only",
	},
	"proton mail protected reply": {
		"proton mail protected reply 9fK2pQ7xNv4mB8 --eo-password-file /run/secrets/jane --body 'Got them, thanks.'",
		"proton mail protected reply 9fK2pQ7xNv4mB8 --eo-password-file /run/secrets/jane --body 'Signed copy attached.' --attach ./signed.pdf",
	},
	"proton mail protected attachments list": {
		"proton mail protected attachments list 9fK2pQ7xNv4mB8 --eo-password-file /run/secrets/jane",
	},
	"proton mail protected attachments download": {
		"proton mail protected attachments download 9fK2pQ7xNv4mB8 --eo-password-file /run/secrets/jane --dest-dir .",
		"proton mail protected attachments download 9fK2pQ7xNv4mB8 q3-report.pdf --eo-password-file /run/secrets/jane --dest report.pdf",
	},

	// ── mail: settings ──
	"proton mail settings get":  {"proton mail settings get"},
	"proton mail settings list": {"proton mail settings list"},
	"proton mail settings set": {
		"proton mail settings set pm-signature off",
		"proton mail settings set view-mode conversations",
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
	"proton mail settings labels reorder": {
		"proton mail settings labels reorder Work",
		"proton mail settings labels reorder --alphabetical",
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
		"proton mail settings folders update 2026 --parent none",
		"proton mail settings folders update Receipts --notify",
	},
	"proton mail settings folders reorder": {
		"proton mail settings folders reorder Receipts",
		"proton mail settings folders reorder --alphabetical",
	},
	"proton mail settings folders delete": {"proton mail settings folders delete Receipts"},
	"proton mail settings addresses list": {"proton mail settings addresses list"},
	"proton mail settings addresses get":  {"proton mail settings addresses get me@proton.me"},
	"proton mail settings addresses update": {
		"proton mail settings addresses update me@proton.me --display-name 'Roman'",
		"proton mail settings addresses update me@proton.me --signature - --html",
		"proton mail settings addresses update me@proton.me --clear-signature",
	},
	"proton mail settings addresses create": {
		"proton mail settings addresses create work@example.com",
		"proton mail settings addresses create work@example.com --display-name Work",
		"proton mail settings addresses create alice@pm.me",
	},
	"proton mail settings addresses reorder": {
		"proton mail settings addresses reorder alice@pm.me",
		"proton mail settings addresses reorder alice@pm.me work@example.com alice@proton.me",
	},
	"proton mail settings addresses enable":  {"proton mail settings addresses enable work@example.com"},
	"proton mail settings addresses disable": {"proton mail settings addresses disable work@example.com"},
	"proton mail settings addresses delete":  {"proton mail settings addresses delete work@example.com"},
	"proton mail settings domains list":      {"proton mail settings domains list"},
	"proton mail settings domains get":       {"proton mail settings domains get example.com"},
	"proton mail settings domains create":    {"proton mail settings domains create example.com"},
	"proton mail settings domains update": {
		"proton mail settings domains update example.com --catch-all work@example.com",
		"proton mail settings domains update example.com --catch-all none",
	},
	"proton mail settings domains delete": {"proton mail settings domains delete example.com"},
	"proton mail settings filters apply": {
		"proton mail settings filters apply",
		"proton mail settings filters apply Newsletters",
	},
	"proton mail settings filters reorder": {
		"proton mail settings filters reorder Receipts",
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
	"proton mail settings senders remove": {
		"proton mail settings senders remove billing@example.com",
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
		"proton mail settings autoreply set --repeat permanent --body 'Away until Monday.'",
		"proton mail settings autoreply set --body 'On holiday.' --start 2026-07-01T09:00 --end 2026-07-14T17:00",
	},
	"proton mail settings autoreply enable":  {"proton mail settings autoreply enable"},
	"proton mail settings autoreply disable": {"proton mail settings autoreply disable"},
	"proton mail settings forwarding list":   {"proton mail settings forwarding list"},
	"proton mail settings forwarding get":    {"proton mail settings forwarding get jane@proton.me"},
	"proton mail settings forwarding create": {
		"proton mail settings forwarding create me@proton.me jane@proton.me",
		"proton mail settings forwarding create me@proton.me jane@example.com",
	},
	"proton mail settings forwarding accept":  {"proton mail settings forwarding accept jane@proton.me"},
	"proton mail settings forwarding decline": {"proton mail settings forwarding decline jane@proton.me"},
	"proton mail settings forwarding enable":  {"proton mail settings forwarding enable jane@proton.me"},
	"proton mail settings forwarding disable": {"proton mail settings forwarding disable jane@proton.me"},
	"proton mail settings forwarding resend":  {"proton mail settings forwarding resend jane@proton.me"},
	"proton mail settings forwarding delete":  {"proton mail settings forwarding delete jane@proton.me"},
	"proton mail settings smtp-tokens list":   {"proton mail settings smtp-tokens list"},
	"proton mail settings smtp-tokens get":    {"proton mail settings smtp-tokens get 'Office printer'"},
	"proton mail settings smtp-tokens create": {
		"proton mail settings smtp-tokens create billing@example.com --name 'Office printer'",
		"proton mail settings smtp-tokens create billing@example.com --name 'Office printer' --output json",
	},
	"proton mail settings smtp-tokens delete": {"proton mail settings smtp-tokens delete 'Office printer'"},
	"proton mail settings imports list":       {"proton mail settings imports list"},
	"proton mail settings imports get":        {"proton mail settings imports get jane@fastmail.com"},
	"proton mail settings imports create": {
		"proton mail settings imports create jane@fastmail.com --imap-password-file /run/secrets/fastmail",
		"proton mail settings imports create jane@fastmail.com --imap-password-file - --to jane@proton.me",
		"proton mail settings imports create jane@fastmail.com --imap-password-file /run/secrets/fastmail --after 2024-01-01 --label Fastmail",
		"proton mail settings imports create jane@fastmail.com --imap-password-file /run/secrets/fastmail --skip Spam --skip 'Archive/*'",
		"proton mail settings imports create jane@example.com --imap-password-file /run/secrets/mailbox --server imap.example.com --port 993",
	},
	"proton mail settings imports cancel": {"proton mail settings imports cancel jane@fastmail.com"},
	"proton mail settings imports resume": {
		"proton mail settings imports resume jane@fastmail.com",
		"proton mail settings imports resume jane@fastmail.com --imap-password-file /run/secrets/fastmail",
	},
	"proton mail settings imports undo":   {"proton mail settings imports undo jane@fastmail.com"},
	"proton mail settings imports delete": {"proton mail settings imports delete jane@fastmail.com"},

	// ── pass ──
	"proton pass vaults share add": {
		"proton pass vaults share add Work jane@proton.me",
		"proton pass vaults share add Work jane@proton.me --access editor",
	},
	"proton pass vaults share confirm": {
		"proton pass vaults share confirm Work jane@example.com",
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
	"proton pass items share confirm": {
		"proton pass items share confirm github.com jane@example.com",
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
		"proton pass settings domains list --output json",
	},
	"proton pass settings domains get": {
		"proton pass settings domains get example.com",
		"proton pass settings domains get example.com --output json",
	},
	"proton pass settings domains create": {
		"proton pass settings domains create example.com",
	},
	"proton pass settings domains update": {
		"proton pass settings domains update example.com --default",
		"proton pass settings domains update example.com --catch-all me@proton.me",
		"proton pass settings domains update example.com --catch-all none",
		"proton pass settings domains update example.com --display-name \"Jane Roe\" --random-prefix",
	},
	"proton pass settings domains delete": {
		"proton pass settings domains delete example.com",
	},
	"proton pass settings access-tokens list": {
		"proton pass settings access-tokens list",
		"proton pass settings access-tokens list --output json",
	},
	"proton pass settings access-tokens get": {
		"proton pass settings access-tokens get ci",
	},
	"proton pass settings access-tokens create": {
		"proton pass settings access-tokens create --name ci --expires 30d --vault Work",
		"proton pass settings access-tokens create --name agent --expires 1d --vault Work --agent",
		"proton pass settings access-tokens create --name ci --expires 30d --vault Work --output json",
	},
	"proton pass settings access-tokens update": {
		"proton pass settings access-tokens update ci --vault Work --vault Personal",
	},
	"proton pass settings access-tokens delete": {
		"proton pass settings access-tokens delete ci",
	},
	"proton pass settings access-tokens activity list": {
		"proton pass settings access-tokens activity list agent",
		"proton pass settings access-tokens activity list agent --limit 20",
	},
	"proton pass settings extra-password get": {
		"proton pass settings extra-password get",
	},
	"proton pass settings extra-password enable": {
		"proton pass settings extra-password enable",
		"proton pass settings extra-password enable --extra-password-file /run/secrets/proton-pass",
	},
	"proton pass settings extra-password disable": {
		"proton pass settings extra-password disable",
		"proton pass settings extra-password disable --extra-password-file /run/secrets/proton-pass",
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
	"proton pass breaches create": {
		"proton pass breaches create me@example.com",
	},
	"proton pass breaches verify": {
		"proton pass breaches verify me@example.com --code 123456",
	},
	"proton pass breaches resend": {
		"proton pass breaches resend me@example.com",
	},
	"proton pass breaches delete": {
		"proton pass breaches delete me@example.com",
	},
	"proton pass breaches enable": {
		"proton pass breaches enable jane@proton.me",
	},
	"proton pass breaches disable": {
		"proton pass breaches disable jane@proton.me",
	},
	"proton pass export": {
		"proton pass export --dest pass-backup.zip --passphrase-file ~/.backup-passphrase",
		"proton pass export --dest pass-backup.zip",
		"proton pass export --dest pass-backup.zip --no-attachments",
		"proton pass export --format csv --dest pass.csv",
		"proton pass export --format json --dest -",
	},
	"proton pass import": {
		"proton pass import pass-backup.zip --passphrase-file ~/.backup-passphrase",
		"proton pass import --dry-run pass-backup.zip",
		"proton pass import bitwarden-export.json --manager bitwarden",
		"proton pass import chrome-passwords.csv --manager chrome --vault Personal",
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
		"proton pass items list --risk reused",
		"proton pass items list --risk weak --vault Work",
		"proton pass items list --risk missing-2fa",
		"proton pass items list --risk compromised",
	},
	"proton pass items get": {
		"proton pass items get github.com",
		"proton pass items get GitHub --output json",
	},
	"proton pass items attachments list": {
		"proton pass items attachments list github.com",
		"proton pass items attachments list github.com --removed",
	},
	"proton pass items attachments download": {
		"proton pass items attachments download github.com --dest-dir .",
		"proton pass items attachments download github.com passport.pdf --dest ~/passport.pdf",
	},
	"proton pass items attachments update": {
		"proton pass items attachments update github.com passport.pdf --name passport-2031.pdf",
	},
	"proton pass items attachments restore": {
		"proton pass items attachments restore github.com passport.pdf",
	},
	"proton pass items passkeys list": {
		"proton pass items passkeys list github.com",
	},
	"proton pass items passkeys remove": {
		"proton pass items passkeys remove github.com roman",
	},
	"proton pass items create": {
		"proton pass items create --name GitHub --username roman --url github.com --generate-password",
		"proton pass items create --type note --name Passport --attach ~/scans/passport.pdf",
		"proton pass items create --name Router --generate-password --words 5",
		"proton pass items create --type note --name 'Door codes' --note 'Front: 1234'",
		"proton pass items create --type credit-card --name 'Travel card' --holder 'Roman' --expiry 2030-04 --secret-file number=/run/secrets/card",
		"proton pass items create --type custom --name Router --field 'Network/SSID=home' --secret-file 'Network/Key=/run/secrets/wifi'",
	},
	"proton pass items update": {
		"proton pass items update GitHub --secret-file password=/run/secrets/github",
		"proton pass items update GitHub --secret-file password=-",
		"proton pass items update GitHub --username roman-16 --url github.com",
		"proton pass items update GitHub --generate-password",
		"proton pass items update Passport --attach ~/scans/visa.pdf --detach passport.pdf",
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
	"proton pass items revisions restore": {
		"proton pass items revisions restore github.com 3",
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
		"proton pass vaults update Work --description 'Shared team logins' --icon star --color teal",
	},
	"proton pass vaults delete": {"proton pass vaults delete Work"},
	"proton pass aliases list":  {"proton pass aliases list", "proton pass aliases list --vault Work"},
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
		"proton update --dry-run",
		"proton update 1.9.11",
		"proton update --force",
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
