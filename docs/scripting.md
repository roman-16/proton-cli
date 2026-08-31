# Scripting

Data goes to stdout, everything else to stderr, and exit codes say what went wrong. See [Output](output.md) for the details.

## Capturing new IDs

Commands that create something print the new ID to stdout, so a plain assignment works:

```bash
LABEL=$(proton mail settings labels create --name Work --color purple)
VAULT=$(proton pass vaults create --name Automation)
MSG=$(proton mail messages send --to me@proton.me --subject Deploy --body "Done.")
```

## JSON and `jq`

```bash
# every unread subject
proton mail messages list --unread --output json | jq -r '.messages[].subject'

# senders of everything older than a week, deduplicated
proton mail messages list --before 2026-04-08 --folder all --page-size 200 --output json | jq -r '.messages[].from_address' | sort -u

# total size of a Drive folder
proton drive items list /Backup --output json | jq '[.items[].size] | add'

# every vault name
proton pass vaults list --output json | jq -r '.vaults[].name'

# today's agenda, one line per event
day=$(date +%F)
proton calendar events list --start "$day" --end "$day" --output json |
  jq -r '.events[] | if .all_day then "all day  \(.title)" else "\(.start[11:16])    \(.title)" end'
```

Every list is an object keyed by its plural name, always with a `count`:

```bash
proton mail messages list --output json | jq '.count'
proton drive items list /Backup --output json | jq -r '.items[].name'
proton pass vaults list --output json | jq -r '.vaults[].name'
```

Keys are `snake_case`, IDs are always complete, and enumerated values are names rather than numbers: `"type": "file"`, not `"type": 2`.

## Archiving mail to disk

`export` writes ordinary RFC 822 `.eml` files.

```bash
# a year of archive, one .eml per message, named "<date> <subject>.eml"
proton mail messages export --folder archive --older-than 1y --all --output-dir ./mail-backup

# a whole folder as a single mbox, ready for Thunderbird or mutt
proton mail messages export --folder inbox --all --format mbox --output inbox.mbox

# one message straight into another tool
proton mail messages export "Invoice #2291" --output - | formail -X ""

# metadata and bodies only, skipping attachment downloads - much faster
proton mail messages export --folder all --all --no-attachments --output-dir ./index
```

Exported files are not encrypted, so put them somewhere you would be comfortable putting the mail itself.

The reverse direction reads a file back into a draft or a send:

```bash
proton mail drafts create --eml ./message.eml
proton mail messages send --eml ./message.eml --to someone-else@proton.me
```

## Answering mail from a script

```bash
# acknowledge everything unread from a sender, then archive it
proton mail messages list --from alerts@example.com --unread --folder all --output json | jq -r '.messages[].id' | while read -r id; do
      proton mail messages reply "$id" --body "Received, thanks." --no-signature
      proton mail messages move "$id" --into archive
    done
```

## Exit codes as control flow

A mutation reports itself structurally too, which is easier to check than parsing a sentence:

```bash
proton mail messages trash --older-than 1y --output json | jq '.count'
```

```bash
if proton pass items get "deploy-key" >/dev/null 2>&1; then
  echo "secret exists"
fi

proton contacts get jane
case $? in
  0) echo "found" ;;
  3) echo "no such contact" ;;
  4) echo "ambiguous, be more specific" ;;
esac
```

## Streaming instead of temporary files

A single `-` means stdin for an input and stdout for an output:

```bash
# back up a database straight into Drive
pg_dump mydb | gzip | proton drive items upload - /Backups/db.sql.gz

# restore it without landing on disk
proton drive items download /Backups/db.sql.gz --output - | gunzip | psql mydb

# mail a report generated on the fly
generate-report | proton mail messages send --to team@example.com --subject "Nightly report" --body -

# encrypt something else with your own tooling on the way out
proton drive items download /report.pdf --output - | gpg --encrypt --recipient me > report.pdf.gpg
```

## Recipes

### Desktop notifications (mail and calendar reminders)

`mail messages watch` and `calendar reminders watch` stay attached and print a line the moment something happens, so whatever shows notifications on your machine reads from them:

```bash
# Linux: every arrival becomes a desktop notification
proton mail messages watch --output json |
  jq --unbuffered -r '[.from_name, .subject] | @tsv' |
  while IFS=$'\t' read -r from subject; do notify-send "$from" "$subject"; done

# macOS
proton mail messages watch --output json |
  jq --unbuffered -r '[.from_name, .subject] | @tsv' |
  while IFS=$'\t' read -r from subject; do
    osascript -e "display notification \"$subject\" with title \"$from\""
  done

# Windows (PowerShell, with the BurntToast module)
proton mail messages watch --output json | jq --unbuffered -c . | ForEach-Object {
  New-BurntToastNotification -Text $_.from_name, $_.subject
}
```

Calendar reminders carry a ready-made sentence in `says`, so theirs is shorter:

```bash
proton calendar reminders watch --output json |
  jq --unbuffered -r .says |
  while read -r line; do notify-send "Reminder" "$line"; done
```

A watcher reports what happens while it is watching; it never replays what arrived before it started. It asks Proton for changes at the same interval its web client does.

### Watching under systemd

```ini
# ~/.config/systemd/user/proton-mail-watch.service
[Unit]
Description=Proton Mail arrivals

[Service]
Environment=PROTON_NO_INPUT=1
ExecStart=/bin/sh -c 'proton mail messages watch --quiet --output json \
  | jq --unbuffered -r "[.from_name, .subject] | @tsv" \
  | while IFS=$(printf "\t") read -r f s; do notify-send "$f" "$s"; done'
Restart=always

[Install]
WantedBy=default.target
```

```ini
# ~/.config/systemd/user/proton-reminders-watch.service
[Unit]
Description=Proton Calendar reminders

[Service]
Environment=PROTON_NO_INPUT=1
ExecStart=/bin/sh -c 'proton calendar reminders watch --quiet --output json \
  | jq --unbuffered -r .says \
  | while read -r line; do notify-send Reminder "$line"; done'
Restart=always

[Install]
WantedBy=default.target
```

`systemctl --user enable --now proton-mail-watch proton-reminders-watch` starts both. A watch stops cleanly on SIGTERM, so `systemctl --user stop` is not logged as a failure.

Which folders count as an arrival is a setting of its own: `mail settings folders list` shows it per folder in NOTIFY, and `folders create`/`update` take `--notify`. Without `--folder`, the watch covers the inbox plus every folder marked that way.

### Nightly backup (cron)

```cron
0 3 * * * /usr/local/bin/proton-backup >/dev/null
```

```bash
#!/usr/bin/env bash
# /usr/local/bin/proton-backup
set -euo pipefail

# Signing in again as the same account does nothing, so running this every time
# costs nothing and recovers on its own from a session that expired.
proton account login --user me@proton.me --password-file ~/.proton-pw
proton drive items upload --recursive /var/backups /Backups
```

### Keep the inbox tidy

```bash
#!/usr/bin/env bash
set -euo pipefail

# archive read newsletters older than a week
proton mail messages move --into archive --from newsletter@example.com --older-than 7d

# bin anything left in spam after a month
proton mail messages delete --folder spam --older-than 30d --yes
```

Run it once with `--dry-run` appended to each command before trusting it.

The `--yes` is not optional there. A cron job has no terminal, so permanent removals, filter-selected changes, external communication, sharing, and network or security-sensitive changes refuse rather than wait for an answer nobody can give. See [When it asks first](language.md#when-it-asks-first).

### Systemd timer

```ini
# ~/.config/systemd/user/proton-backup.service
[Service]
Type=oneshot
Environment=PROTON_NO_INPUT=1
LoadCredential=proton:%h/.proton-pw
ExecStart=/usr/bin/proton account login --user me@proton.me --password-file %d/proton
ExecStart=/usr/bin/proton drive items upload --recursive %h/Documents /Backups
```

```ini
# ~/.config/systemd/user/proton-backup.timer
[Timer]
OnCalendar=daily
Persistent=true

[Install]
WantedBy=timers.target
```

### Out of office

```bash
proton mail settings autoreply set --repeat fixed --start "$(date -d 'next monday 09:00' +%Y-%m-%dT%H:%M)" --end "$(date -d 'next friday 18:00' +%Y-%m-%dT%H:%M)" --message "Away this week. For anything urgent, contact team@example.com."

# and when you are back
proton mail settings autoreply disable
```

### Alias-per-signup

```bash
alias() {
  proton pass aliases create --prefix "$1" --mailbox me@proton.me
}
alias newsletter-xyz
```

## Automation notes

- **Credentials**: an account is attached to a profile by `account login`. Hand the password over with `--password-file`, from a path only your user can read - systemd's `LoadCredential=`, Kubernetes secrets and Docker secrets all give you one.
- **2FA**: `--totp` is only consulted during a fresh login. For unattended jobs, sign in once interactively so the session file exists, then let the job reuse it.
- **Elevation**: Proton asks for the password again before `calendar settings calendars delete`, `mail messages expire` and `mail settings autoreply set`. A session cannot answer for it, so those commands take `--password-file` and `--password-stdin` of their own.
- **CAPTCHA**: a login on a headless machine can hit human verification, which needs a desktop. Log in on a desktop first and copy the session, or run the job somewhere with a display. See [Human verification](human-verification.md).
- **`--quiet`** silences the `✓` lines and progress bars, useful in cron.
- **Bad moments upstream**: a 502 from Proton's edge or a connection that fails is waited out and asked again - for anything that only reads, and for signing in. Nothing that changes something is ever sent twice. A failure that outlasts the waiting exits 5, so a job can tell "Proton is having trouble, come back later" from "the password is wrong" (exit 2).
- **Rate limits**: bulk commands page through Proton's API and respect its caps (150 messages per page). Long-running loops should sleep between iterations.
- **Search lag**: Proton's index is eventually consistent, so a just-sent message may not appear in `list` for a few seconds. Act on the ID that the command printed instead of searching again.
