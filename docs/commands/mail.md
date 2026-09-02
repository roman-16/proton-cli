# proton mail

Read, write and organize mail.

Every command under `proton mail`, with the arguments and flags it takes. For these commands in use, see [the guide](../apps/mail.md).

Holds `conversations`, `drafts`, `messages` and `settings`.

## `conversations`

Whole threads.

Holds `attachments`, `delete`, `export`, `forward`, `get`, `label`, `list`, `mark`, `move`, `reply`, `snooze`, `star`, `trash`, `unlabel`, `unsnooze` and `unstar`.

### `conversations attachments`

Files attached anywhere in a thread.

Holds `download` and `list`.

### `conversations attachments download`

Download and decrypt attachments from a thread.

```
proton mail conversations attachments download REF [ATTACHMENT_REF]
```

```bash
proton mail conversations attachments download 'Quarterly numbers' --dest-dir .
```

| Flag | Description |
| --- | --- |
| `--dest string` | Write to this path, or - for stdout |
| `--dest-dir string` | Write into this directory, keeping each item's own name |
| `--force` | Overwrite a file that already exists |
| `--include-inline` | Include inline attachments |

### `conversations attachments list`

List every attachment in a thread.

```
proton mail conversations attachments list REF
```

```bash
proton mail conversations attachments list 'Quarterly numbers'
```

| Flag | Description |
| --- | --- |
| `--include-inline` | Include inline attachments |

### `conversations delete`

Delete threads permanently.

```
proton mail conversations delete [REF...]
```

```bash
proton mail conversations delete 5bH2mQxK
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

### `conversations export`

Write a whole thread out as .eml files or one mbox.

```
proton mail conversations export REF
```

```bash
proton mail conversations export 'Quarterly numbers' --dest-dir ./backup
```

| Flag | Description |
| --- | --- |
| `--dest string` | Write to this path, or - for stdout |
| `--dest-dir string` | Write into this directory, keeping each item's own name |
| `--force` | Overwrite a file that already exists |
| `--format string` | How to lay the thread down: eml, mbox (default `mbox`) |
| `--no-attachments` | Skip attachments |

### `conversations forward`

Forward the newest message in a thread.

```
proton mail conversations forward REF
```

```bash
proton mail conversations forward 'Quarterly numbers' --to jane@example.com
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

### `conversations get`

Show a whole thread, decrypted.

```
proton mail conversations get REF
```

```bash
proton mail conversations get 'Quarterly numbers'
proton mail conversations get 5bH2mQxK --summary
```

| Flag | Description |
| --- | --- |
| `--body-only` | Emit only the bodies, with no headers or dividers |
| `--include-inline` | List inline attachments too |
| `--render string` | Which representation of the body to print: text, html, raw (default `text`) |
| `--strip-quotes` | Drop quoted reply blocks from each body |
| `--summary` | One line per message instead of the full thread |

### `conversations label`

Attach a label to threads.

```
proton mail conversations label [REF...]
```

```bash
proton mail conversations label 'Quarterly numbers' --label Accounting
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

### `conversations list`

List threads in a folder.

The filters are the ones every organising verb takes, so a selection can be read here and then handed to one of them. Text predicates go through Proton's index, which lags a change by a few seconds.

It looks in the inbox unless told otherwise; --folder all searches everything.

```
proton mail conversations list
```

```bash
proton mail conversations list
proton mail conversations list --unread --folder inbox
proton mail conversations list --from jane@example.com --folder all
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
| `--page-size int` | How many threads per page (default `25`) |
| `--starred` | Match starred messages |
| `--subject string` | Match text in the subject |
| `--to string` | Match a recipient's address |
| `--unread` | Match unread messages |

### `conversations mark`

Set whether threads count as read.

Holds `read` and `unread`.

### `conversations mark read`

Mark threads as read.

```
proton mail conversations mark read [REF...]
```

```bash
proton mail conversations mark read 'Quarterly numbers'
proton mail conversations mark read --folder inbox --all
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

### `conversations mark unread`

Mark threads as unread.

```
proton mail conversations mark unread [REF...]
```

```bash
proton mail conversations mark unread 'Quarterly numbers'
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

### `conversations move`

Move threads to a folder.

```
proton mail conversations move [REF...]
```

```bash
proton mail conversations move 'Quarterly numbers' --into archive
proton mail conversations move --older-than 90d --folder inbox --into archive
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

### `conversations reply`

Reply to the newest message in a thread.

```
proton mail conversations reply REF
```

```bash
proton mail conversations reply 'Quarterly numbers' --body 'Looks right to me.'
proton mail conversations reply 'Quarterly numbers' --everyone --body Agreed.
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

### `conversations snooze`

Take threads out of the inbox until later.

--until takes a duration from now, such as 3d, or a moment written out in full, such as 2026-04-17T09:00. The thread returns to the inbox then, unread.

```
proton mail conversations snooze [REF...]
```

```bash
proton mail conversations snooze 5bH2mQxK --until 3d
proton mail conversations snooze --unread --until 2026-04-17T09:00
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
| `--until string` | When they come back (e.g. 3d, or 2026-04-17T09:00) |

### `conversations star`

Star threads.

```
proton mail conversations star [REF...]
```

```bash
proton mail conversations star 'Quarterly numbers'
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

### `conversations trash`

Move threads to the trash.

```
proton mail conversations trash [REF...]
```

```bash
proton mail conversations trash 'Quarterly numbers'
proton mail conversations trash --from newsletter@example.com --older-than 90d
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

### `conversations unlabel`

Detach a label from threads.

```
proton mail conversations unlabel [REF...]
```

```bash
proton mail conversations unlabel 'Quarterly numbers' --label Accounting
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

### `conversations unsnooze`

Bring snoozed threads back to the inbox now.

```
proton mail conversations unsnooze [REF...]
```

```bash
proton mail conversations unsnooze 5bH2mQxK
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

### `conversations unstar`

Remove the star from threads.

```
proton mail conversations unstar [REF...]
```

```bash
proton mail conversations unstar 'Quarterly numbers'
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

## `drafts`

Messages not yet sent.

Holds `create`, `delete`, `list`, `send` and `update`.

### `drafts create`

Save a draft without sending it.

```
proton mail drafts create
```

```bash
proton mail drafts create --to team@example.com --subject Standup --body 'Notes to follow.'
proton mail drafts create --to jane@example.com --subject Report --attach ./report.pdf
```

| Flag | Description |
| --- | --- |
| `--attach stringArray` | File to attach (repeatable) |
| `--attach-inline stringArray` | Image to embed in the HTML body by Content-ID (repeatable; needs --html) |
| `--bcc stringArray` | Blind-carbon-copy recipient (repeatable) |
| `--body string` | Message body (- reads stdin) |
| `--cc stringArray` | Carbon-copy recipient (repeatable) |
| `--eml string` | Build the message from an RFC 822 file; other flags override what it says |
| `--from string` | Address to send from, by email or ID (default: your primary) |
| `--html` | Treat the body as HTML rather than plain text |
| `--no-signature` | Leave out this address's signature and Proton's footer |
| `--subject string` | Subject line |
| `--to stringArray` | Recipient (repeatable; accepts "Name <addr>") |

### `drafts delete`

Delete drafts.

```
proton mail drafts delete REF...
```

```bash
proton mail drafts delete 5bH2mQxK
```

### `drafts list`

List drafts.

```
proton mail drafts list
```

```bash
proton mail drafts list
```

| Flag | Description |
| --- | --- |
| `--page int` | Which page of results, counting from zero |
| `--page-size int` | How many drafts per page (default `25`) |

### `drafts send`

Send a draft as it stands.

Its body already contains whatever signature it was created with, so nothing is appended.

```
proton mail drafts send REF
```

```bash
proton mail drafts send 5bH2mQxK
proton mail drafts send 5bH2mQxK --send-at 2026-04-16T09:00
```

| Flag | Description |
| --- | --- |
| `--eo-password-file string` | Read the password for recipients outside Proton from a file |
| `--eo-password-hint string` | Hint shown to password-protected recipients |
| `--eo-password-stdin` | Read the password for recipients outside Proton from stdin |
| `--expires string` | Self-destruct after DURATION (e.g. 7d, 24h), or never |
| `--send-at string` | Schedule delivery (RFC 3339, or YYYY-MM-DDTHH:MM in the zone you are working in) |

### `drafts update`

Change a draft. Only what you pass is replaced; everything else is kept.

--to, --cc and --bcc replace the whole list rather than adding to it. --attach adds files and --detach removes one by name or ID.

```
proton mail drafts update REF
```

```bash
proton mail drafts update 5bH2mQxK --body 'Notes attached.'
proton mail drafts update 5bH2mQxK --detach report.pdf
```

| Flag | Description |
| --- | --- |
| `--attach stringArray` | File to attach (repeatable) |
| `--attach-inline stringArray` | Image to embed in the HTML body by Content-ID (repeatable; needs --html) |
| `--bcc stringArray` | Blind-carbon-copy recipient (repeatable) |
| `--body string` | Message body (- reads stdin) |
| `--cc stringArray` | Carbon-copy recipient (repeatable) |
| `--detach stringArray` | Remove an attachment by name or ID (repeatable) |
| `--from string` | Address to send from, by email or ID (default: your primary) |
| `--html` | Switch the draft to text/html |
| `--no-signature` | Leave out this address's signature and Proton's footer |
| `--plain` | Switch the draft to text/plain |
| `--subject string` | Subject line |
| `--to stringArray` | Recipient (repeatable; accepts "Name <addr>") |

## `messages`

Individual messages.

Holds `attachments`, `delete`, `empty`, `expire`, `export`, `forward`, `get`, `label`, `list`, `mark`, `move`, `reply`, `send`, `star`, `trash`, `unlabel`, `unschedule`, `unstar`, `unsubscribe` and `watch`.

### `messages attachments`

Files attached to a message.

Holds `download` and `list`.

### `messages attachments download`

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

### `messages attachments list`

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

### `messages delete`

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

### `messages empty`

Delete everything in a folder, permanently.

Nothing is listed first: Proton clears the folder without naming what was in it, which is why this cannot be narrowed and why it always asks.

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

### `messages expire`

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

### `messages export`

Write messages out as standalone RFC 822 documents, readable by any mail client, grep, or anything else.

eml writes one file per message; mbox concatenates everything into one stream. Skipping attachments with --no-attachments is much faster for a large archive.

Exported files are not encrypted - that is what exporting means. The original DKIM signatures will not verify against the rebuilt body either, exactly as with the web client's own export.

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

### `messages forward`

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

### `messages get`

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

### `messages label`

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

### `messages list`

List messages in a folder.

The filters are the ones every organising verb takes, so a selection can be read here and then handed to trash, move, label or export. Text predicates go through Proton's index, which lags a change by a few seconds.

It looks in the inbox unless told otherwise; --folder all searches everything.

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

### `messages mark`

Set whether messages count as read.

Holds `read` and `unread`.

### `messages mark read`

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

### `messages mark unread`

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

### `messages move`

Move messages to a folder.

A folder is somewhere a message lives, so moving takes it out of wherever it was. To add a tag while leaving it in place, use `label` instead.

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

### `messages reply`

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

### `messages send`

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

### `messages star`

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

### `messages trash`

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

### `messages unlabel`

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

### `messages unschedule`

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

### `messages unstar`

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

### `messages unsubscribe`

Ask a mailing list to stop.

Proton does the asking, using whatever the message offered - a List-Unsubscribe header, or the one-click form behind it - because Proton is the party the list already knows.

```
proton mail messages unsubscribe REF...
```

```bash
proton mail messages unsubscribe 5bH2mQxK
```

### `messages watch`

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

## `settings`

How Mail behaves.

Holds `addresses`, `autoreply`, `filters`, `folders`, `get`, `labels`, `list`, `senders` and `set`.

### `settings addresses`

Your addresses, display names and signatures.

Holds `get`, `list` and `update`.

### `settings addresses get`

Show one address, including its signature.

```
proton mail settings addresses get REF
```

```bash
proton mail settings addresses get me@proton.me
```

### `settings addresses list`

List the addresses on the account.

```
proton mail settings addresses list
```

```bash
proton mail settings addresses list
```

### `settings addresses update`

Set the display name recipients see and the signature appended to mail sent from this address.

Proton stores signatures as HTML. Plain text is escaped and its newlines become line breaks; --html passes markup through untouched.

```
proton mail settings addresses update REF
```

```bash
proton mail settings addresses update me@proton.me --display-name 'Roman'
proton mail settings addresses update me@proton.me --signature - --html
proton mail settings addresses update me@proton.me --clear-signature
```

| Flag | Description |
| --- | --- |
| `--clear-signature` | Remove the signature |
| `--display-name string` | Name recipients see next to the address |
| `--html` | Treat the signature as HTML rather than escaping it |
| `--signature string` | Signature appended to mail from this address (- reads stdin) |

### `settings autoreply`

The automatic reply and its schedule.

Holds `disable`, `enable`, `get` and `set`.

### `settings autoreply disable`

Turn the auto-reply off, keeping its schedule.

```
proton mail settings autoreply disable
```

```bash
proton mail settings autoreply disable
```

### `settings autoreply enable`

Turn the auto-reply on, keeping its schedule.

```
proton mail settings autoreply enable
```

```bash
proton mail settings autoreply enable
```

### `settings autoreply get`

Show the auto-reply and its schedule.

```
proton mail settings autoreply get
```

```bash
proton mail settings autoreply get
```

### `settings autoreply set`

Configure the auto-reply and turn it on.

--start and --end are written in the grammar the repeat mode dictates: fixed      2026-07-01T09:00   a date and time in your zone daily      09:00              a time of day, with --days weekly     mon:09:00          a weekday and time monthly    1:09:00            a day of the month and time permanent  -                  no bounds

Proton sends every auto-reply with the subject "Auto" and offers no way to change it. Auto-reply is a paid feature.

```
proton mail settings autoreply set
```

```bash
proton mail settings autoreply set --repeat permanent --message 'Away until Monday.'
proton mail settings autoreply set --message 'On holiday.' --start 2026-07-01T09:00 --end 2026-07-14T17:00
```

| Flag | Description |
| --- | --- |
| `--days stringSlice` | Days it is active, for a daily schedule, e.g. mon,tue,wed |
| `--end string` | End of the window (grammar depends on --repeat) |
| `--html` | Treat the message as HTML rather than escaping it |
| `--message string` | Reply body (- reads stdin) |
| `--password-file string` | Read the account password from a file |
| `--password-stdin` | Read the account password from stdin |
| `--repeat string` | How the schedule repeats: fixed, daily, weekly, monthly, permanent (default `fixed`) |
| `--start string` | Start of the window (grammar depends on --repeat) |
| `--totp string` | Two-factor code |

### `settings filters`

Server-side Sieve filters.

Holds `apply`, `create`, `delete`, `disable`, `enable`, `get`, `list`, `reorder` and `update`.

### `settings filters apply`

Run filters over mail that is already in the mailbox.

A filter ordinarily acts once, as mail arrives, so a rule written today does nothing about what came yesterday.

With no filter named, every enabled filter runs.

```
proton mail settings filters apply [REF...]
```

```bash
proton mail settings filters apply
proton mail settings filters apply Newsletters
```

### `settings filters create`

Create a filter.

Describe it with --if and the actions below, and Proton writes the Sieve. A condition reads FIELD [not] COMPARATOR VALUE:

  field       subject, sender, recipient, attachments
  comparator  contains, is, starts, ends, matches

`is` wants the whole value; `matches` takes * and ? as wildcards. An attachments condition takes no value - it asks whether there is one.

--sieve takes a script you wrote yourself instead.

```
proton mail settings filters create
```

```bash
proton mail settings filters create --name Receipts --if "sender contains billing@" --label Receipts
proton mail settings filters create --name Big --sieve ./big.sieve
```

| Flag | Description |
| --- | --- |
| `--disabled` | Create it without turning it on |
| `--if stringArray` | A condition matching mail must meet, as FIELD [not] COMPARATOR VALUE (repeatable) |
| `--label stringArray` | Apply this label to matching mail (repeatable) |
| `--mark-read` | Mark matching mail as read |
| `--match string` | Whether every condition must hold, or any one of them: all, any (default `all`) |
| `--move-to string` | Move matching mail into this folder (archive, inbox, spam, trash, or one of yours) |
| `--name string` | Name for the new filter |
| `--sieve string` | Sieve script (- reads stdin) |
| `--star` | Star matching mail |

### `settings filters delete`

Delete filters.

```
proton mail settings filters delete REF...
```

```bash
proton mail settings filters delete Receipts
```

### `settings filters disable`

Disable filters.

```
proton mail settings filters disable REF...
```

```bash
proton mail settings filters disable Receipts
```

### `settings filters enable`

Enable filters.

```
proton mail settings filters enable REF...
```

```bash
proton mail settings filters enable Receipts
```

### `settings filters get`

Show what a filter matches and does.

```
proton mail settings filters get REF
```

```bash
proton mail settings filters get Receipts
```

### `settings filters list`

List your filters.

```
proton mail settings filters list
```

```bash
proton mail settings filters list
```

### `settings filters reorder`

Set the order filters run in.

The first rule to file a message wins, so the order decides where mail lands. Name every filter, in the order you want them: this replaces the whole order rather than nudging one entry, because a half-stated order is one nobody can predict.

```
proton mail settings filters reorder REF...
```

```bash
proton mail settings filters reorder Newsletters Receipts Archive
```

### `settings filters update`

Change what a filter is called, matches, or does.

--if and the actions beside it replace the whole rule rather than adding to it. The filter keeps its place in the order and whether it is running.

```
proton mail settings filters update REF
```

```bash
proton mail settings filters update Receipts --sieve ./receipts.sieve
```

| Flag | Description |
| --- | --- |
| `--if stringArray` | A condition matching mail must meet, as FIELD [not] COMPARATOR VALUE (repeatable) |
| `--label stringArray` | Apply this label to matching mail (repeatable) |
| `--mark-read` | Mark matching mail as read |
| `--match string` | Whether every condition must hold, or any one of them: all, any (default `all`) |
| `--move-to string` | Move matching mail into this folder (archive, inbox, spam, trash, or one of yours) |
| `--name string` | New name |
| `--sieve string` | New Sieve script (- reads stdin) |
| `--star` | Star matching mail |

### `settings folders`

Folders, which a message lives in.

Holds `create`, `delete`, `list` and `update`.

### `settings folders create`

Create a folder.

```
proton mail settings folders create
```

```bash
proton mail settings folders create --name Receipts
proton mail settings folders create --name 2026 --parent Receipts --color olive
proton mail settings folders create --name Receipts --notify=false
```

| Flag | Description |
| --- | --- |
| `--color string` | Accent color, by name (purple) or hex (#8080FF) (default `#8080FF`) |
| `--name string` | Name for the new folder |
| `--notify` | Tell you when mail arrives here (default `true`) |
| `--parent string` | Put it inside this folder, by ID |

### `settings folders delete`

Delete folders.

```
proton mail settings folders delete REF...
```

```bash
proton mail settings folders delete Receipts
```

### `settings folders list`

List your folders.

```
proton mail settings folders list
```

```bash
proton mail settings folders list
```

### `settings folders update`

Rename or recolor a folder.

```
proton mail settings folders update REF
```

```bash
proton mail settings folders update Receipts --name Invoices
proton mail settings folders update Receipts --notify
```

| Flag | Description |
| --- | --- |
| `--color string` | New accent color, as a hex value |
| `--name string` | New name |
| `--notify` | Tell you when mail arrives here (default `true`) |
| `--parent string` | Move it inside this folder, by ID |

### `settings get`

Show the mail settings now in effect.

```
proton mail settings get
```

```bash
proton mail settings get
```

### `settings labels`

Labels, which a message carries.

Holds `create`, `delete`, `list` and `update`.

### `settings labels create`

Create a label.

```
proton mail settings labels create
```

```bash
proton mail settings labels create --name Work
proton mail settings labels create --name Accounting --color pacific
```

| Flag | Description |
| --- | --- |
| `--color string` | Accent color, by name (purple) or hex (#8080FF) (default `#8080FF`) |
| `--name string` | Name for the new label |

### `settings labels delete`

Delete labels.

```
proton mail settings labels delete REF...
```

```bash
proton mail settings labels delete Work
```

### `settings labels list`

List your labels.

```
proton mail settings labels list
```

```bash
proton mail settings labels list
```

### `settings labels update`

Rename or recolor a label.

```
proton mail settings labels update REF
```

```bash
proton mail settings labels update Work --name Office
proton mail settings labels update Work --color enzian
```

| Flag | Description |
| --- | --- |
| `--color string` | New accent color, as a hex value |
| `--name string` | New name |

### `settings list`

List the mail settings that can be changed.

```
proton mail settings list
```

```bash
proton mail settings list
```

### `settings senders`

Who always reaches the inbox, and who never does.

Holds `allow`, `block`, `forget`, `list` and `spam`.

### `settings senders allow`

Always let someone reach the inbox.

A whole domain works too, written with the @: `@example.com`.

Deciding again about the same sender replaces the earlier decision rather than colliding with it.

```
proton mail settings senders allow EMAIL...
```

```bash
proton mail settings senders allow billing@example.com
```

### `settings senders block`

Send someone's mail straight to blocked.

A whole domain works too, written with the @: `@example.com`.

Deciding again about the same sender replaces the earlier decision rather than colliding with it.

```
proton mail settings senders block EMAIL...
```

```bash
proton mail settings senders block spammer@example.com
proton mail settings senders block @example.com
```

### `settings senders forget`

Drop a standing decision, letting the spam filter decide again.

```
proton mail settings senders forget EMAIL...
```

```bash
proton mail settings senders forget billing@example.com
```

### `settings senders list`

List every standing decision about a sender.

```
proton mail settings senders list
```

```bash
proton mail settings senders list
```

### `settings senders spam`

Send someone's mail straight to spam.

A whole domain works too, written with the @: `@example.com`.

Deciding again about the same sender replaces the earlier decision rather than colliding with it.

```
proton mail settings senders spam EMAIL...
```

```bash
proton mail settings senders spam newsletter@example.com
```

### `settings set`

Change one mail setting.

```
proton mail settings set KEY VALUE
```

```bash
proton mail settings set signature off
proton mail settings set view-mode conversation
```

---

Every command also takes the [flags that work everywhere](README.md#flags-that-work-on-every-command).
