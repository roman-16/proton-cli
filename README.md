# proton-cli

> **Disclaimer:** This is an unofficial, community-built tool and is not endorsed by or affiliated with Proton AG. Use at your own risk.

An unofficial command-line tool for [Proton](https://proton.me) — Mail, Drive, Calendar, Pass, and Contacts from your terminal.

Implements the same authentication and encryption as the [Proton web client](https://github.com/ProtonMail/WebClients): SRP login, PGP key hierarchy, and full end-to-end encryption using [go-srp](https://github.com/ProtonMail/go-srp) and [gopenpgp](https://github.com/ProtonMail/gopenpgp).

## Install

### Download a binary (recommended)

Grab the latest binary for your platform from [**GitHub Releases**](https://github.com/roman-16/proton-cli/releases/latest).

| Platform | Binary |
|---|---|
| Linux (x86_64) | `proton-cli_VERSION_linux_amd64` |
| Linux (ARM64) | `proton-cli_VERSION_linux_arm64` |
| macOS (Apple Silicon) | `proton-cli_VERSION_darwin_arm64` |
| macOS (Intel) | `proton-cli_VERSION_darwin_amd64` |
| Windows (x86_64) | `proton-cli_VERSION_windows_amd64.exe` |
| Windows (ARM64) | `proton-cli_VERSION_windows_arm64.exe` |

**Linux / macOS:**

```bash
curl -LO https://github.com/roman-16/proton-cli/releases/latest/download/proton-cli_VERSION_linux_amd64
chmod +x proton-cli_*
sudo mv proton-cli_* /usr/local/bin/proton-cli
```

**Windows:** download the `.exe` from the [releases page](https://github.com/roman-16/proton-cli/releases/latest) and add it to your PATH.

### Install with Go

```bash
go install github.com/roman-16/proton-cli@latest
```

### Build from source

```bash
git clone https://github.com/roman-16/proton-cli.git
cd proton-cli
go build .
```

## Quick Start

### 1. Set your credentials

```bash
export PROTON_USER=alice@proton.me
export PROTON_PASSWORD='your-password'
# export PROTON_TOTP=123456   # if 2FA is enabled
```

The session is saved to `~/.config/proton-cli/sessions/<profile>.json` and reused automatically. The raw `api` command works without a password; encrypted commands require it.

### 2. Try it

```bash
proton-cli mail messages list
proton-cli drive items list
proton-cli --help
```

## Conventions

- **REF** — anywhere you see `REF` in usage, you can pass either a full Proton ID or a search term (subject/name/URL/title depending on the command). Ambiguous matches print candidates to stderr and exit with code 4.
- **Exit codes** — `0` success · `1` user error · `2` auth · `3` not-found · `4` conflict / ambiguous · `5` network / server.
- **Output** — `--output text|json|yaml` (default `text`). Mutations print `✓ …` to stderr and the new ID to stdout so `ID=$(proton-cli ...)` works.
- **Streaming I/O** — `-` means stdin (inputs) or stdout (outputs). `mail messages send --body -`, `drive items upload - /path`, `drive items download /path -`.
- **Cancellation** — `Ctrl+C` aborts in-flight operations.
- **Dry run** — `--dry-run` on every mutating command previews without applying.

## Commands

### Mail

```bash
# Messages
proton-cli mail messages list
proton-cli mail messages list --folder sent
proton-cli mail messages list --folder drafts --page 1 --page-size 10 --unread
proton-cli mail messages search --keyword "invoice"
proton-cli mail messages search --from "amazon" --after 2026-01-01
proton-cli mail messages read REF
proton-cli mail messages read REF --format text            # text|html|raw
proton-cli mail messages send --to "to@ex.com" --subject "Hi" --body "Hello"
echo "body" | proton-cli mail messages send --to foo --subject bar --body -
proton-cli mail messages trash REF...
proton-cli mail messages delete REF...                     # permanent
proton-cli mail messages move --dest archive REF...
proton-cli mail messages mark read REF                     # ACTION: read|unread
proton-cli mail messages mark unread REF
proton-cli mail messages star REF
proton-cli mail messages unstar REF

# Batch filters (union with any explicit REFs)
proton-cli mail messages trash --unread --older-than 30d
proton-cli mail messages move --dest archive --from "newsletter@" --older-than 7d
proton-cli mail messages mark read --folder inbox --unread
proton-cli mail messages delete --folder spam --all

# Attachments
proton-cli mail attachments list MESSAGE_ID
proton-cli mail attachments download MESSAGE_ID ATTACHMENT_ID ./file.pdf
proton-cli mail attachments download MESSAGE_ID ATTACHMENT_ID -   # stdout

# Labels and folders
proton-cli mail labels list
proton-cli mail labels create --name "Important" --color "#8080FF"
proton-cli mail labels create --name "Projects" --folder --color "#1DA583"
proton-cli mail labels delete LABEL_ID

# Filters
proton-cli mail filters list
proton-cli mail filters create --name "Archive invoices" \
  --sieve 'require ["fileinto"]; if header :contains "Subject" "invoice" { fileinto "Archive"; }'
proton-cli mail filters enable FILTER_ID
proton-cli mail filters disable FILTER_ID
proton-cli mail filters delete FILTER_ID

# Addresses
proton-cli mail addresses list
```

### Drive

```bash
# Items
proton-cli drive items list
proton-cli drive items list /Documents
proton-cli drive items upload ./photo.jpg                  # to root
proton-cli drive items upload ./report.pdf /Documents      # into a folder
proton-cli drive items upload - /Notes/note.txt            # from stdin
proton-cli drive items upload --recursive ./folder /Backup
proton-cli drive items download /Documents/report.pdf ./report.pdf
proton-cli drive items download /Photos/pic.jpg            # to stdout
proton-cli drive items download /Photos/pic.jpg -          # to stdout (explicit)
proton-cli drive items rename /Documents/old.txt new.txt
proton-cli drive items move /Documents/report.pdf /Archive
proton-cli drive items delete /Documents/old-report.pdf
proton-cli drive items delete --permanent /Documents/secret.txt

# Batch filters
proton-cli drive items delete --pattern "*.tmp" --recursive --scope /
proton-cli drive items delete --larger-than 100MB --scope /Backups --recursive
proton-cli drive items delete --older-than 90d --scope /Logs --recursive
proton-cli drive items delete --scope /OldStuff --all --recursive

# Folders
proton-cli drive folders create /Documents/NewFolder

# Trash
proton-cli drive trash list
proton-cli drive trash restore LINK_ID LINK_ID2
proton-cli drive trash empty
```

### Calendar

```bash
# Calendars
proton-cli calendar calendars list
proton-cli calendar calendars create --name "Work" --color "#8080FF"
proton-cli calendar calendars delete CALENDAR_ID           # requires password

# Events
proton-cli calendar events list
proton-cli calendar events list --start 2026-04-15 --end 2026-04-20
proton-cli calendar events list --calendar "Work"
proton-cli calendar events get CALENDAR_ID EVENT_ID
proton-cli calendar events get "Meeting"                   # search by title
proton-cli calendar events create \
  --title "Meeting" --location "Vienna" \
  --start "2026-04-16T14:00" --duration 1h
proton-cli calendar events update CALENDAR_ID EVENT_ID --title "Updated"
proton-cli calendar events delete CALENDAR_ID EVENT_ID
proton-cli calendar events delete "Meeting"                # search by title
```

### Contacts

```bash
proton-cli contacts list
proton-cli contacts get REF                                # ID or search
proton-cli contacts create --name "John Doe" --email "john@example.com" --phone "+1234567890"
proton-cli contacts update REF --email "new@example.com"
proton-cli contacts delete REF
```

### Pass

```bash
# Items
proton-cli pass items list
proton-cli pass items list --vault "Work"
proton-cli pass items get SHARE_ID ITEM_ID
proton-cli pass items get "github.com"                     # search
proton-cli pass items create --type login --name "GitHub" --username me --password secret --url github.com
proton-cli pass items create --type note --name "My Note" --note "Some text"
proton-cli pass items create --type card --name "Visa" --holder "Roman" --number "4111..." --expiry "2028-12"
proton-cli pass items edit REF --password "new-secret"
proton-cli pass items trash REF
proton-cli pass items restore REF
proton-cli pass items delete REF

# Batch filters
proton-cli pass items trash --vault "Old" --type login
proton-cli pass items trash --older-than 1y --type login
proton-cli pass items delete --vault "Temporary" --all

# Vaults
proton-cli pass vaults list
proton-cli pass vaults create --name "Work"
proton-cli pass vaults delete SHARE_ID

# Aliases
proton-cli pass alias options
proton-cli pass alias create --prefix my-alias --mailbox my-mailbox@proton.me
```

### Settings

```bash
proton-cli settings get      # account settings
proton-cli settings mail     # mail settings
```

### Raw API

For any endpoint not covered by high-level commands:

```bash
proton-cli api GET /drive/volumes
proton-cli api POST /calendar/v1 --body '{"Name":"Work",...}'
proton-cli api GET /mail/v4/messages --query Page=0 --query PageSize=10
proton-cli api GET /calendar/v1 --output json | jq '.Calendars[].ID'
```

## Profiles (multi-account)

Create `~/.config/proton-cli/config.toml`:

```toml
default_profile = "default"

[profiles.default]
user = "alice@proton.me"

[profiles.work]
user = "alice@company.com"
api_url = "https://mail.proton.me/api"
```

Then:

```bash
proton-cli --profile work mail messages list
```

Each profile gets its own session file at `~/.config/proton-cli/sessions/<profile>.json`.

## Environment Variables

| Variable | Description |
|---|---|
| `PROTON_USER` | Proton account email |
| `PROTON_PASSWORD` | Account password (required for encrypted operations) |
| `PROTON_TOTP` | TOTP code (if 2FA is enabled) |
| `PROTON_API_URL` | API base URL (default: `https://mail.proton.me/api`) |
| `PROTON_APP_VERSION` | App version header (default: `Other`) |

Flags override env vars; env vars override profile values.

## How It Works

1. **Session creation** — creates an unauthenticated session via `POST /auth/v4/sessions`.
2. **SRP authentication** — Secure Remote Password login with [go-srp](https://github.com/ProtonMail/go-srp), with 2FA/TOTP support.
3. **Session persistence** — saves tokens + salted key password per profile.
4. **Key hierarchy** — unlocks User key → Address keys → per-service keys (Calendar, Drive, Contacts).
5. **End-to-end encryption** — encrypts/decrypts using [gopenpgp](https://github.com/ProtonMail/gopenpgp).
6. **Auto-refresh** — refreshes expired tokens automatically.

### Encryption Details

| Service | Encrypt with | Sign with |
|---|---|---|
| Calendar events | Calendar key (session key) | Address key |
| Drive files | Node key (session key per block) | Address key |
| Drive names | Parent node key | Address key |
| Contacts | User key | User key |
| Mail | Session key | Address key |
| Pass items | AES-256-GCM (item key) | N/A (symmetric) |
| Pass vaults | AES-256-GCM (vault key) | N/A (symmetric) |

## API Reference

See [`openapi.yaml`](openapi.yaml) for the complete API spec covering ~740 endpoints. To regenerate from the latest Proton source:

```bash
cd scripts && npm install && npm run generate-openapi
```

See [`scripts/README.md`](scripts/README.md) for details on the generator.

## Development

Requires [devbox](https://www.jetify.com/devbox) and [direnv](https://direnv.net/):

```bash
direnv allow
go build .
```

## License

MIT
