# proton-cli

A command-line tool for interacting with the Proton API (Drive, Calendar, Mail, Contacts). Handles SRP authentication and end-to-end encryption automatically.

## Install

```bash
go install github.com/roman-16/proton-cli@latest
```

Or build from source:

```bash
git clone https://github.com/roman-16/proton-cli.git
cd proton-cli
direnv allow
go build .
```

## Usage

### Authentication

```bash
export PROTON_USER=alice@proton.me
export PROTON_PASSWORD='your-password'
# export PROTON_TOTP=123456  # if 2FA is enabled
```

The session is saved to `~/.config/proton-cli/session.json` and reused automatically.

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
```

### Calendar

```bash
# List events
proton-cli calendar list-events
proton-cli calendar list-events --start 2026-04-15 --end 2026-04-20
proton-cli calendar list-events --calendar "Work"

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

# Get event details
proton-cli calendar get-event CALENDAR_ID EVENT_ID
```

### Contacts

```bash
# List contacts (decrypted)
proton-cli contacts list

# Get contact details
proton-cli contacts get CONTACT_ID

# Create contact
proton-cli contacts create --name "John Doe" --email "john@example.com" --phone "+1234567890"
```

### Mail

```bash
# Read a message (decrypted)
proton-cli mail read MESSAGE_ID

# Send a message
proton-cli mail send --to "recipient@example.com" --subject "Hello" --body "Message text"
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
proton-cli drive ls /Documents | head -5
```

## API Reference

See [`openapi.yaml`](openapi.yaml) for the complete API spec covering ~740 endpoints. To regenerate from the latest Proton source:

```bash
cd scripts && npm install && npm run generate-openapi
```

See [`scripts/README.md`](scripts/README.md) for details on the generator.

## How It Works

1. **Session creation** — creates an unauthenticated session via `POST /auth/v4/sessions` (same as Proton web clients)
2. **SRP authentication** — Secure Remote Password login using [go-srp](https://github.com/ProtonMail/go-srp), with 2FA/TOTP support
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

## Environment Variables

| Variable | Description |
|---|---|
| `PROTON_USER` | Proton account email |
| `PROTON_PASSWORD` | Account password (needed for encrypted operations) |
| `PROTON_TOTP` | TOTP code (if 2FA is enabled) |
| `PROTON_API_URL` | API base URL (default: `https://mail.proton.me/api`) |
| `PROTON_APP_VERSION` | App version header (default: `web-account@5.0.364.0`) |

## Development

Requires [devbox](https://www.jetify.com/devbox) and [direnv](https://direnv.net/):

```bash
direnv allow
go build .
go test ./...
```

## License

MIT
