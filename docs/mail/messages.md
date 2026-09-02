# proton mail messages

Individual messages.

Every command under `proton mail messages`, with the arguments and flags it takes. For these commands in use, see [the mail guide](README.md).

Holds `attachments`, `delete`, `empty`, `expire`, `export`, `forward`, `get`, `label`, `list`, `mark`, `move`, `reply`, `send`, `star`, `trash`, `unlabel`, `unschedule`, `unstar`, `unsubscribe` and `watch`.

## `attachments`

Files attached to a message.

Holds `download` and `list`.

### `attachments download`

Download and decrypt attachments.

Naming an attachment downloads that one; naming none downloads them all. Existing files are never overwritten silently: a collision becomes "file (2).pdf" unless --force says otherwise.

```
proton mail messages attachments download REF [ATTACHMENT_REF]
```

```bash
proton mail messages attachments download 'Invoice #2291' --dest-dir .
proton mail messages attachments download 5bH2mQxK kQ81mDx4 --dest invoice.pdf
```

| Flag | Description |
| --- | --- |
| `--dest string` | Write to this path, or - for stdout |
| `--dest-dir string` | Write into this directory, keeping each item's own name |
| `--force` | Overwrite a file that already exists |
| `--include-inline` | Include inline attachments when downloading them all |

### `attachments list`

List a message's attachments.

```
proton mail messages attachments list REF
```

```bash
proton mail messages attachments list 'Invoice #2291'
proton mail messages attachments list 5bH2mQxK --include-inline
```

| Flag | Description |
| --- | --- |
| `--include-inline` | Include inline attachments, such as signature graphics |

## `delete`

Delete messages permanently.

```
proton mail messages delete [REF...]
```

```bash
proton mail messages delete 5bH2mQxK
proton mail messages delete --folder spam --all --yes
```

| Flag | Description |
| --- | --- |
| `--after string` | Match messages after this date (YYYY-MM-DD) |
| `--all` | Act on everything in scope, rather than a subset |
| `--before string` | Match messages before this date (YYYY-MM-DD) |
| `--folder string` | Folder or label to look in (default: all) |
| `--from string` | Match the sender's address |
| `--keyword string` | Match text anywhere, including display names and bodies |
| `--limit int` | Most messages to affect (Proton pages at 150) (default `150`) |
| `--newer-than string` | Match messages newer than DURATION |
| `--older-than string` | Match messages older than DURATION (e.g. 30d, 2w, 1h) |
| `--starred` | Match starred messages |
| `--subject string` | Match text in the subject |
| `--to string` | Match a recipient's address |
| `--unread` | Match unread messages |

## `empty`

Delete everything in a folder, permanently.

Proton clears the folder without reporting what was in it, so nothing is listed first. This takes no filters and always asks for confirmation.

```
proton mail messages empty
```

```bash
proton mail messages empty --folder trash
proton mail messages empty --folder spam
```

| Flag | Description |
| --- | --- |
| `--folder string` | Folder or label to look in |

## `expire`

Make messages delete themselves after a while, or stop them.

--in takes a duration and Proton stores the moment it lands on, so a message already counting down reports when rather than how long.

```
proton mail messages expire [REF...]
```

```bash
proton mail messages expire 5bH2mQxK --in 7d
proton mail messages expire --from newsletter@example.com --in 30d
proton mail messages expire 5bH2mQxK --never
```

| Flag | Description |
| --- | --- |
| `--after string` | Match messages after this date (YYYY-MM-DD) |
| `--all` | Act on everything in scope, rather than a subset |
| `--before string` | Match messages before this date (YYYY-MM-DD) |
| `--folder string` | Folder or label to look in (default: all) |
| `--from string` | Match the sender's address |
| `--in string` | Delete them after DURATION (e.g. 7d, 24h) |
| `--keyword string` | Match text anywhere, including display names and bodies |
| `--limit int` | Most messages to affect (Proton pages at 150) (default `150`) |
| `--never` | Stop them expiring |
| `--newer-than string` | Match messages newer than DURATION |
| `--older-than string` | Match messages older than DURATION (e.g. 30d, 2w, 1h) |
| `--password-file string` | Read the account password from a file |
| `--password-stdin` | Read the account password from stdin |
| `--starred` | Match starred messages |
| `--subject string` | Match text in the subject |
| `--to string` | Match a recipient's address |
| `--totp string` | Two-factor code |
| `--unread` | Match unread messages |

## `export`

Write messages out as standalone RFC 822 documents, readable by any mail client, grep, or anything else.

eml writes one file per message; mbox concatenates everything into one stream. Skipping attachments with --no-attachments is much faster for a large archive.

Exported files are not encrypted. Their DKIM and ARC headers no longer verify either, since the body those headers signed was the encrypted one. Proton's own web export behaves the same way.

```
proton mail messages export [REF...]
```

```bash
proton mail messages export 'Invoice #2291' --dest-dir ./backup
proton mail messages export --folder archive --all --dest-dir ./mail-backup
proton mail messages export --folder archive --older-than 1y --format mbox --dest archive.mbox
```

| Flag | Description |
| --- | --- |
| `--after string` | Match messages after this date (YYYY-MM-DD) |
| `--all` | Act on everything in scope, rather than a subset |
| `--before string` | Match messages before this date (YYYY-MM-DD) |
| `--dest string` | Write to this path, or - for stdout |
| `--dest-dir string` | Write into this directory, keeping each item's own name |
| `--folder string` | Folder or label to look in (default: all) |
| `--force` | Overwrite a file that already exists |
| `--format string` | How to lay the messages down: eml, mbox (default `eml`) |
| `--from string` | Match the sender's address |
| `--keyword string` | Match text anywhere, including display names and bodies |
| `--limit int` | Most messages to affect (Proton pages at 150) (default `150`) |
| `--newer-than string` | Match messages newer than DURATION |
| `--no-attachments` | Skip attachments, which is much faster |
| `--older-than string` | Match messages older than DURATION (e.g. 30d, 2w, 1h) |
| `--starred` | Match starred messages |
| `--subject string` | Match text in the subject |
| `--to string` | Match a recipient's address |
| `--unread` | Match unread messages |

## `forward`

Forward a message.

The original is quoted below your text with its own headers, the subject gains "Fw:", and its attachments come along without being re-uploaded.

```
proton mail messages forward REF
```

```bash
proton mail messages forward 'Invoice #2291' --to jane@example.com
proton mail messages forward 'Invoice #2291' --to jane@example.com --no-attachments
```

| Flag | Description |
| --- | --- |
| `--attach stringArray` | File to attach (repeatable) |
| `--attach-inline stringArray` | Image to embed in the HTML body by Content-ID (repeatable; needs --html) |
| `--bcc stringArray` | Blind-carbon-copy recipient (repeatable) |
| `--body string` | Your text, placed above the quoted original (- reads stdin) |
| `--cc stringArray` | Carbon-copy recipient (repeatable) |
| `--draft` | Save as a draft instead of sending |
| `--eo-password-file string` | Read the password for recipients outside Proton from a file |
| `--eo-password-hint string` | Hint shown to password-protected recipients |
| `--eo-password-stdin` | Read the password for recipients outside Proton from stdin |
| `--expires string` | Self-destruct after DURATION (e.g. 7d, 24h), or never |
| `--from string` | Address to send from, by email or ID (default: your primary) |
| `--html` | Compose in HTML (default: match the original) |
| `--no-attachments` | Leave the original's attachments behind |
| `--no-quote` | Do not quote the original message |
| `--no-signature` | Leave out this address's signature and Proton's footer |
| `--send-at string` | Schedule delivery (RFC 3339, or YYYY-MM-DDTHH:MM in the zone you are working in) |
| `--to stringArray` | Recipient (repeatable; accepts "Name <addr>") |

## `get`

Show one message, decrypted.

```
proton mail messages get REF
```

```bash
proton mail messages get 'Invoice #2291'
proton mail messages get 5bH2mQxK --render html
proton mail messages get 5bH2mQxK --body-only --strip-quotes
```

| Flag | Description |
| --- | --- |
| `--body-only` | Emit only the body, with no headers or attachment list |
| `--include-inline` | List inline attachments too, such as signature graphics |
| `--render string` | Which representation of the body to print: text, html, raw (default `text`) |
| `--strip-quotes` | Drop quoted reply blocks from the body |

## `label`

Attach a label to messages.

```
proton mail messages label [REF...]
```

```bash
proton mail messages label 'Invoice #2291' --label Accounting
proton mail messages label --from billing@example.com --label Accounting
```

| Flag | Description |
| --- | --- |
| `--after string` | Match messages after this date (YYYY-MM-DD) |
| `--all` | Act on everything in scope, rather than a subset |
| `--before string` | Match messages before this date (YYYY-MM-DD) |
| `--folder string` | Folder or label to look in (default: all) |
| `--from string` | Match the sender's address |
| `--keyword string` | Match text anywhere, including display names and bodies |
| `--label string` | The label to attach or detach, by name or ID |
| `--limit int` | Most messages to affect (Proton pages at 150) (default `150`) |
| `--newer-than string` | Match messages newer than DURATION |
| `--older-than string` | Match messages older than DURATION (e.g. 30d, 2w, 1h) |
| `--starred` | Match starred messages |
| `--subject string` | Match text in the subject |
| `--to string` | Match a recipient's address |
| `--unread` | Match unread messages |

## `list`

List messages in a folder.

Takes the same filters as trash, move, label and export, so you can preview a selection here before acting on it. Text filters go through Proton's index, which lags a change by a few seconds.

Looks in the inbox unless told otherwise. Use --folder all to search everything.

```
proton mail messages list
```

```bash
proton mail messages list
proton mail messages list --unread
proton mail messages list --folder archive --page-size 50
proton mail messages list --starred --output json
proton mail messages list --from billing@example.com --folder all
proton mail messages list --keyword invoice --after 2026-01-01 --folder all
```

| Flag | Description |
| --- | --- |
| `--after string` | Match messages after this date (YYYY-MM-DD) |
| `--before string` | Match messages before this date (YYYY-MM-DD) |
| `--folder string` | Folder or label to look in (default: inbox) |
| `--from string` | Match the sender's address |
| `--keyword string` | Match text anywhere, including display names and bodies |
| `--newer-than string` | Match messages newer than DURATION |
| `--older-than string` | Match messages older than DURATION (e.g. 30d, 2w, 1h) |
| `--page int` | Which page of results, counting from zero |
| `--page-size int` | How many messages per page (default `25`) |
| `--starred` | Match starred messages |
| `--subject string` | Match text in the subject |
| `--to string` | Match a recipient's address |
| `--unread` | Match unread messages |

## `mark`

Set whether messages count as read.

Holds `read` and `unread`.

### `mark read`

Mark messages as read.

```
proton mail messages mark read [REF...]
```

```bash
proton mail messages mark read 'Invoice #2291'
proton mail messages mark read --folder inbox --all
```

| Flag | Description |
| --- | --- |
| `--after string` | Match messages after this date (YYYY-MM-DD) |
| `--all` | Act on everything in scope, rather than a subset |
| `--before string` | Match messages before this date (YYYY-MM-DD) |
| `--folder string` | Folder or label to look in (default: all) |
| `--from string` | Match the sender's address |
| `--keyword string` | Match text anywhere, including display names and bodies |
| `--limit int` | Most messages to affect (Proton pages at 150) (default `150`) |
| `--newer-than string` | Match messages newer than DURATION |
| `--older-than string` | Match messages older than DURATION (e.g. 30d, 2w, 1h) |
| `--starred` | Match starred messages |
| `--subject string` | Match text in the subject |
| `--to string` | Match a recipient's address |
| `--unread` | Match unread messages |

### `mark unread`

Mark messages as unread.

```
proton mail messages mark unread [REF...]
```

```bash
proton mail messages mark unread 'Invoice #2291'
```

| Flag | Description |
| --- | --- |
| `--after string` | Match messages after this date (YYYY-MM-DD) |
| `--all` | Act on everything in scope, rather than a subset |
| `--before string` | Match messages before this date (YYYY-MM-DD) |
| `--folder string` | Folder or label to look in (default: all) |
| `--from string` | Match the sender's address |
| `--keyword string` | Match text anywhere, including display names and bodies |
| `--limit int` | Most messages to affect (Proton pages at 150) (default `150`) |
| `--newer-than string` | Match messages newer than DURATION |
| `--older-than string` | Match messages older than DURATION (e.g. 30d, 2w, 1h) |
| `--starred` | Match starred messages |
| `--subject string` | Match text in the subject |
| `--to string` | Match a recipient's address |
| `--unread` | Match unread messages |

## `move`

Move messages to a folder.

A message is in exactly one folder, so this takes it out of the one it was in. To tag it while leaving it where it is, use `label` instead.

```
proton mail messages move [REF...]
```

```bash
proton mail messages move 'Invoice #2291' --into archive
proton mail messages move --from newsletter@example.com --older-than 90d --into archive
```

| Flag | Description |
| --- | --- |
| `--after string` | Match messages after this date (YYYY-MM-DD) |
| `--all` | Act on everything in scope, rather than a subset |
| `--before string` | Match messages before this date (YYYY-MM-DD) |
| `--folder string` | Folder or label to look in (default: all) |
| `--from string` | Match the sender's address |
| `--into string` | Destination folder, by name or ID |
| `--keyword string` | Match text anywhere, including display names and bodies |
| `--limit int` | Most messages to affect (Proton pages at 150) (default `150`) |
| `--newer-than string` | Match messages newer than DURATION |
| `--older-than string` | Match messages older than DURATION (e.g. 30d, 2w, 1h) |
| `--starred` | Match starred messages |
| `--subject string` | Match text in the subject |
| `--to string` | Match a recipient's address |
| `--unread` | Match unread messages |

## `reply`

Reply to a message.

The original is quoted below your text, the subject gains "Re:", and the reply leaves from the address the original arrived on.

--all includes everyone who was on the message. --draft stops before sending, so you can edit it with `mail drafts update`.

```
proton mail messages reply REF
```

```bash
proton mail messages reply 'Invoice #2291' --body 'Thanks, paid today.'
proton mail messages reply 'Invoice #2291' --everyone --body 'Noted.'
proton mail messages reply 'Invoice #2291' --body 'Draft first.' --draft
```

| Flag | Description |
| --- | --- |
| `--attach stringArray` | File to attach (repeatable) |
| `--attach-inline stringArray` | Image to embed in the HTML body by Content-ID (repeatable; needs --html) |
| `--bcc stringArray` | Blind-carbon-copy recipient (repeatable) |
| `--body string` | Your text, placed above the quoted original (- reads stdin) |
| `--cc stringArray` | Carbon-copy recipient (repeatable) |
| `--draft` | Save as a draft instead of sending |
| `--eo-password-file string` | Read the password for recipients outside Proton from a file |
| `--eo-password-hint string` | Hint shown to password-protected recipients |
| `--eo-password-stdin` | Read the password for recipients outside Proton from stdin |
| `--everyone` | Reply to everyone who was on the message, not just the sender |
| `--expires string` | Self-destruct after DURATION (e.g. 7d, 24h), or never |
| `--from string` | Address to send from, by email or ID (default: your primary) |
| `--html` | Compose in HTML (default: match the original) |
| `--no-quote` | Do not quote the original message |
| `--no-signature` | Leave out this address's signature and Proton's footer |
| `--send-at string` | Schedule delivery (RFC 3339, or YYYY-MM-DDTHH:MM in the zone you are working in) |
| `--to stringArray` | Recipient (repeatable; accepts "Name <addr>") |

## `send`

Compose and send a message.

```
proton mail messages send
```

```bash
proton mail messages send --to jane@example.com --subject Report --body 'See attached.' --attach ./report.pdf
proton mail messages send --to team@example.com --subject Standup --body -
proton mail messages send --to jane@example.com --subject Reminder --send-at 2026-04-16T09:00
proton mail messages send --to jane@example.com --subject Invoice --body 'See attached.' --eo-password-file /run/secrets/jane
proton mail messages send --eml ./draft.eml
```

| Flag | Description |
| --- | --- |
| `--attach stringArray` | File to attach (repeatable) |
| `--attach-inline stringArray` | Image to embed in the HTML body by Content-ID (repeatable; needs --html) |
| `--bcc stringArray` | Blind-carbon-copy recipient (repeatable) |
| `--body string` | Message body (- reads stdin) |
| `--cc stringArray` | Carbon-copy recipient (repeatable) |
| `--eml string` | Build the message from an RFC 822 file; other flags override what it says |
| `--eo-password-file string` | Read the password for recipients outside Proton from a file |
| `--eo-password-hint string` | Hint shown to password-protected recipients |
| `--eo-password-stdin` | Read the password for recipients outside Proton from stdin |
| `--expires string` | Self-destruct after DURATION (e.g. 7d, 24h), or never |
| `--from string` | Address to send from, by email or ID (default: your primary) |
| `--html` | Treat the body as HTML rather than plain text |
| `--no-signature` | Leave out this address's signature and Proton's footer |
| `--send-at string` | Schedule delivery (RFC 3339, or YYYY-MM-DDTHH:MM in the zone you are working in) |
| `--subject string` | Subject line |
| `--to stringArray` | Recipient (repeatable; accepts "Name <addr>") |

## `star`

Star messages.

```
proton mail messages star [REF...]
```

```bash
proton mail messages star 'Invoice #2291'
```

| Flag | Description |
| --- | --- |
| `--after string` | Match messages after this date (YYYY-MM-DD) |
| `--all` | Act on everything in scope, rather than a subset |
| `--before string` | Match messages before this date (YYYY-MM-DD) |
| `--folder string` | Folder or label to look in (default: all) |
| `--from string` | Match the sender's address |
| `--keyword string` | Match text anywhere, including display names and bodies |
| `--limit int` | Most messages to affect (Proton pages at 150) (default `150`) |
| `--newer-than string` | Match messages newer than DURATION |
| `--older-than string` | Match messages older than DURATION (e.g. 30d, 2w, 1h) |
| `--starred` | Match starred messages |
| `--subject string` | Match text in the subject |
| `--to string` | Match a recipient's address |
| `--unread` | Match unread messages |

## `trash`

Move messages to the trash.

```
proton mail messages trash [REF...]
```

```bash
proton mail messages trash 'Invoice #2291'
proton mail messages trash --unread --older-than 30d
proton mail messages trash --from newsletter@example.com --older-than 90d --dry-run
```

| Flag | Description |
| --- | --- |
| `--after string` | Match messages after this date (YYYY-MM-DD) |
| `--all` | Act on everything in scope, rather than a subset |
| `--before string` | Match messages before this date (YYYY-MM-DD) |
| `--folder string` | Folder or label to look in (default: all) |
| `--from string` | Match the sender's address |
| `--keyword string` | Match text anywhere, including display names and bodies |
| `--limit int` | Most messages to affect (Proton pages at 150) (default `150`) |
| `--newer-than string` | Match messages newer than DURATION |
| `--older-than string` | Match messages older than DURATION (e.g. 30d, 2w, 1h) |
| `--starred` | Match starred messages |
| `--subject string` | Match text in the subject |
| `--to string` | Match a recipient's address |
| `--unread` | Match unread messages |

## `unlabel`

Detach a label from messages.

```
proton mail messages unlabel [REF...]
```

```bash
proton mail messages unlabel 'Invoice #2291' --label Accounting
```

| Flag | Description |
| --- | --- |
| `--after string` | Match messages after this date (YYYY-MM-DD) |
| `--all` | Act on everything in scope, rather than a subset |
| `--before string` | Match messages before this date (YYYY-MM-DD) |
| `--folder string` | Folder or label to look in (default: all) |
| `--from string` | Match the sender's address |
| `--keyword string` | Match text anywhere, including display names and bodies |
| `--label string` | The label to attach or detach, by name or ID |
| `--limit int` | Most messages to affect (Proton pages at 150) (default `150`) |
| `--newer-than string` | Match messages newer than DURATION |
| `--older-than string` | Match messages older than DURATION (e.g. 30d, 2w, 1h) |
| `--starred` | Match starred messages |
| `--subject string` | Match text in the subject |
| `--to string` | Match a recipient's address |
| `--unread` | Match unread messages |

## `unschedule`

Cancel a scheduled send.

The message leaves the queue and returns to Drafts, keeping its ID - the same thing the web client's "Edit and reschedule" does. To change the time, cancel it and send again with --send-at.

```
proton mail messages unschedule [REF...]
```

```bash
proton mail messages unschedule 5bH2mQxK
proton mail messages unschedule --all
```

| Flag | Description |
| --- | --- |
| `--all` | Act on everything in scope, rather than a subset |

## `unstar`

Remove the star from messages.

```
proton mail messages unstar [REF...]
```

```bash
proton mail messages unstar 'Invoice #2291'
```

| Flag | Description |
| --- | --- |
| `--after string` | Match messages after this date (YYYY-MM-DD) |
| `--all` | Act on everything in scope, rather than a subset |
| `--before string` | Match messages before this date (YYYY-MM-DD) |
| `--folder string` | Folder or label to look in (default: all) |
| `--from string` | Match the sender's address |
| `--keyword string` | Match text anywhere, including display names and bodies |
| `--limit int` | Most messages to affect (Proton pages at 150) (default `150`) |
| `--newer-than string` | Match messages newer than DURATION |
| `--older-than string` | Match messages older than DURATION (e.g. 30d, 2w, 1h) |
| `--starred` | Match starred messages |
| `--subject string` | Match text in the subject |
| `--to string` | Match a recipient's address |
| `--unread` | Match unread messages |

## `unsubscribe`

Ask a mailing list to stop.

Proton sends the request on your behalf, using whatever the message offered: a List-Unsubscribe header, or the one-click form behind it.

```
proton mail messages unsubscribe REF...
```

```bash
proton mail messages unsubscribe 5bH2mQxK
```

## `watch`

Print each message as it arrives, until you stop it.

It reports what happens while it is watching, so nothing that arrived beforehand comes up. A thread returning from snooze counts as arriving.

Without --folder it covers the inbox plus every folder whose notifications are on, which `settings folders list` shows under NOTIFY.

```
proton mail messages watch
```

```bash
proton mail messages watch
proton mail messages watch --folder all
proton mail messages watch --from billing@example.com
proton mail messages watch --output json
```

| Flag | Description |
| --- | --- |
| `--folder string` | Folder or label to look in (default: the ones that notify) |
| `--from string` | Match the sender's address |
| `--subject string` | Match text in the subject |

---

Every command also takes the [flags that work everywhere](../about/commands.md#flags-that-work-on-every-command).
