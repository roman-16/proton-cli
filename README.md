# proton-cli

A command-line tool for [Proton](https://proton.me) — Mail, Drive, Calendar, and Contacts from your terminal.

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
# Download (replace URL with the latest release)
curl -LO https://github.com/roman-16/proton-cli/releases/latest/download/proton-cli_VERSION_linux_amd64

# Make executable and move to PATH
chmod +x proton-cli_*
sudo mv proton-cli_* /usr/local/bin/proton-cli
```

**Windows:** download the `.exe` from the [releases page](https://github.com/roman-16/proton-cli/releases/latest) and add it to your PATH.

### Install with Go

If you have Go installed:

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
# export PROTON_TOTP=123456  # if 2FA is enabled
```

The session is saved to `~/.config/proton-cli/session.json` and reused automatically. The raw `api` command works without a password; encrypted commands (mail read, drive, calendar, contacts) require it.

### 2. Try it

```bash
# List your inbox
proton-cli mail list

# Read a message
proton-cli mail read MESSAGE_ID

# List your drive files
proton-cli drive ls

# See all commands
proton-cli --help
```

## Commands

### Mail

```bash
# List messages
proton-cli mail list
proton-cli mail list --folder sent
proton-cli mail list --folder drafts --page 1 --page-size 10
proton-cli mail list --unread

# Search messages
proton-cli mail search --keyword "invoice"
proton-cli mail search --from "amazon" --after 2026-01-01
proton-cli mail search --subject "order" --folder inbox --limit 50

# Read a message (decrypted)
proton-cli mail read MESSAGE_ID

# Send a message
proton-cli mail send --to "recipient@example.com" --subject "Hello" --body "Message text"

# Move, trash, delete
proton-cli mail move --folder archive MESSAGE_ID
proton-cli mail trash MESSAGE_ID
proton-cli mail delete MESSAGE_ID                      # permanent

# Mark messages
proton-cli mail mark --read MESSAGE_ID
proton-cli mail mark --unread MESSAGE_ID
proton-cli mail mark --starred MESSAGE_ID
proton-cli mail mark --unstar MESSAGE_ID

# Attachments
proton-cli mail attachments list MESSAGE_ID
proton-cli mail attachments download MESSAGE_ID ATTACHMENT_ID ./output.pdf

# Labels and folders
proton-cli mail labels list
proton-cli mail labels create --name "Important" --color "#8080FF"
proton-cli mail labels create --name "Projects" --folder --color "#1DA583"
proton-cli mail labels delete LABEL_ID

# Addresses
proton-cli mail addresses list

# Filters
proton-cli mail filters list
proton-cli mail filters create --name "Archive invoices" \
  --sieve 'require ["fileinto"]; if header :contains "Subject" "invoice" { fileinto "Archive"; }'
proton-cli mail filters enable FILTER_ID
proton-cli mail filters disable FILTER_ID
proton-cli mail filters delete FILTER_ID
```

### Drive

```bash
# List files
proton-cli drive ls
proton-cli drive ls /Documents
proton-cli drive ls /Documents/Projects

# Create folders
proton-cli drive mkdir /Documents/NewFolder

# Upload files
proton-cli drive upload ./photo.jpg                    # to root
proton-cli drive upload ./report.pdf /Documents        # to subfolder

# Download files
proton-cli drive download /Documents/report.pdf ./report.pdf
proton-cli drive download /Photos/pic.jpg              # to stdout

# Rename
proton-cli drive rename /Documents/old-name.txt new-name.txt

# Move
proton-cli drive mv /Documents/report.pdf /Archive

# Delete (move to trash)
proton-cli drive rm /Documents/old-report.pdf
proton-cli drive rm --permanent /Documents/secret.txt  # permanent delete

# Trash management
proton-cli drive trash list
proton-cli drive trash restore LINK_ID
proton-cli drive trash empty
```

### Calendar

```bash
# List calendars
proton-cli calendar list

# List events
proton-cli calendar list-events
proton-cli calendar list-events --start 2026-04-15 --end 2026-04-20
proton-cli calendar list-events --calendar "Work"

# Get event details (by IDs or title search)
proton-cli calendar get-event CALENDAR_ID EVENT_ID
proton-cli calendar get-event "Meeting"

# Create event
proton-cli calendar create-event \
  --title "Meeting" \
  --location "Vienna" \
  --start "2026-04-16T14:00" \
  --duration 1h

# Update event
proton-cli calendar update-event CALENDAR_ID EVENT_ID \
  --title "Updated Meeting" \
  --location "Graz"

# Delete event (by IDs or title search)
proton-cli calendar delete-event CALENDAR_ID EVENT_ID
proton-cli calendar delete-event "Meeting"

# Create / delete calendars
proton-cli calendar create --name "Work" --color "#8080FF"
proton-cli calendar delete CALENDAR_ID
```

### Contacts

```bash
# List contacts
proton-cli contacts list

# Get contact (by ID or name/email search)
proton-cli contacts get CONTACT_ID
proton-cli contacts get "John Doe"
proton-cli contacts get "john@example"

# Create contact
proton-cli contacts create --name "John Doe" --email "john@example.com" --phone "+1234567890"

# Update contact
proton-cli contacts update "John Doe" --email "new@example.com" --phone "+0987654321"

# Delete contact
proton-cli contacts delete "John Doe"
proton-cli contacts delete CONTACT_ID
```

### Settings

```bash
proton-cli settings get       # account settings
proton-cli settings mail      # mail settings
```

### Raw API Access

For any endpoint not covered by the high-level commands:

```bash
proton-cli api GET /calendar/v1
proton-cli api GET /drive/volumes
proton-cli api POST /calendar/v1 --body '{"Name":"Work","Color":"#7272a7","Display":1,"AddressID":"..."}'
proton-cli api DELETE /calendar/v1/CALENDAR_ID
proton-cli api GET /mail/v4/messages --query Page=0 --query PageSize=10
```

Pipe through `jq` for formatting:

```bash
proton-cli api GET /calendar/v1 | jq '.Calendars[].ID'
```

## Output Format

All commands default to human-readable table output. Pass `--json` for machine-readable JSON:

```bash
proton-cli mail list --json
proton-cli contacts list --json
proton-cli calendar list-events --json
proton-cli drive ls --json
```

## Environment Variables

| Variable | Description |
|---|---|
| `PROTON_USER` | Proton account email |
| `PROTON_PASSWORD` | Account password (needed for encrypted operations) |
| `PROTON_TOTP` | TOTP code (if 2FA is enabled) |
| `PROTON_API_URL` | API base URL (default: `https://mail.proton.me/api`) |
| `PROTON_APP_VERSION` | App version header (default: `web-account@5.0.364.0`) |

## How It Works

1. **Session creation** — creates an unauthenticated session via `POST /auth/v4/sessions`
2. **SRP authentication** — Secure Remote Password login with [go-srp](https://github.com/ProtonMail/go-srp), with 2FA/TOTP support
3. **Session persistence** — saves tokens + salted key password to `~/.config/proton-cli/session.json`
4. **Key hierarchy** — unlocks User key → Address keys → per-service keys (Calendar, Drive, Contacts)
5. **End-to-end encryption** — encrypts/decrypts using [gopenpgp](https://github.com/ProtonMail/gopenpgp)
6. **Auto-refresh** — refreshes expired tokens automatically

### Encryption Details

| Service | Encrypt with | Sign with |
|---|---|---|
| Calendar events | Calendar key (session key) | Address key |
| Drive files | Node key (session key per block) | Address key |
| Drive names | Parent node key | Address key |
| Contacts | User key | User key |
| Mail | Session key | Address key |

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
go test ./...
```

## License

MIT
