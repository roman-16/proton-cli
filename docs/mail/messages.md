# proton mail messages

Individual messages.

Every command under `proton mail messages`, with the arguments and flags it takes. For these commands in use, see [the mail guide](README.md).

Holds `attachments`, `delete`, `empty`, `export`, `forward`, `get`, `label`, `list`, `mark`, `move`, `receipt`, `reply`, `send`, `star`, `trash`, `unlabel`, `unschedule`, `unstar`, `unsubscribe`, `update` and `watch`.

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
proton mail messages attachments download 'Invoice #2291' invoice-2291.pdf --dest-dir .
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
| `--after string` | First day to include (YYYY-MM-DD) |
| `--all` | Act on everything in scope, rather than a subset |
| `--before string` | Last day to include (YYYY-MM-DD) |
| `--folder string` | Folder or label to look in (default: all) |
| `--from string` | Match the sender's address |
| `--has-attachments` | Match messages with attachments |
| `--keyword string` | Match text in the subject, a name or an address, and in bodies once a mail index exists |
| `--limit int` | Most messages to affect; 0 for no cap (default `150`) |
| `--newer-than string` | Match messages newer than DURATION |
| `--older-than string` | Match messages older than DURATION (e.g. 30d, 2w, 1h) |
| `--read` | Match read messages |
| `--starred` | Match starred messages |
| `--subject string` | Match text in the subject |
| `--to string` | Match a recipient's address |
| `--unread` | Match unread messages |
| `--via string` | Match mail that arrived at, or left from, this address of yours |

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
| `--after string` | First day to include (YYYY-MM-DD) |
| `--all` | Act on everything in scope, rather than a subset |
| `--before string` | Last day to include (YYYY-MM-DD) |
| `--dest string` | Write to this path, or - for stdout |
| `--dest-dir string` | Write into this directory, keeping each item's own name |
| `--folder string` | Folder or label to look in (default: all) |
| `--force` | Overwrite a file that already exists |
| `--format string` | How to lay the messages down: eml, mbox (default `eml`) |
| `--from string` | Match the sender's address |
| `--has-attachments` | Match messages with attachments |
| `--keyword string` | Match text in the subject, a name or an address, and in bodies once a mail index exists |
| `--limit int` | Most messages to affect; 0 for no cap (default `150`) |
| `--newer-than string` | Match messages newer than DURATION |
| `--no-attachments` | Skip attachments, which is much faster |
| `--older-than string` | Match messages older than DURATION (e.g. 30d, 2w, 1h) |
| `--read` | Match read messages |
| `--starred` | Match starred messages |
| `--subject string` | Match text in the subject |
| `--to string` | Match a recipient's address |
| `--unread` | Match unread messages |
| `--via string` | Match mail that arrived at, or left from, this address of yours |

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
| `--eo-password-file string` | Read the password for recipients outside Proton from a file, or - for stdin |
| `--eo-password-hint string` | Hint shown to password-protected recipients |
| `--expires string` | Self-destruct after DURATION (e.g. 7d, 24h), or never |
| `--from string` | Address to send from, by email or ID (default: your primary) |
| `--html` | Compose in HTML (default: match the original) |
| `--no-attachments` | Leave the original's attachments behind |
| `--no-quote` | Do not quote the original message |
| `--no-signature` | Leave out this address's signature and Proton's footer |
| `--request-receipt` | Ask the recipients to confirm when they read it |
| `--send-at string` | Schedule delivery (RFC 3339, or YYYY-MM-DDTHH:MM in the zone you are working in) |
| `--to stringArray` | Recipient (repeatable; accepts "Name <addr>") |

## `get`

Show one message, decrypted.

A message Proton flagged carries a Flagged line reading phishing or suspicious; `mark legitimate` overrules it. DMARC: failed means the sender's domain did not vouch for the message, so the address it claims to come from may not be the address it came from.

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
| `--after string` | First day to include (YYYY-MM-DD) |
| `--all` | Act on everything in scope, rather than a subset |
| `--before string` | Last day to include (YYYY-MM-DD) |
| `--folder string` | Folder or label to look in (default: all) |
| `--from string` | Match the sender's address |
| `--has-attachments` | Match messages with attachments |
| `--keyword string` | Match text in the subject, a name or an address, and in bodies once a mail index exists |
| `--label string` | The label to attach or detach, by name or ID |
| `--limit int` | Most messages to affect; 0 for no cap (default `150`) |
| `--newer-than string` | Match messages newer than DURATION |
| `--older-than string` | Match messages older than DURATION (e.g. 30d, 2w, 1h) |
| `--read` | Match read messages |
| `--starred` | Match starred messages |
| `--subject string` | Match text in the subject |
| `--to string` | Match a recipient's address |
| `--unread` | Match unread messages |
| `--via string` | Match mail that arrived at, or left from, this address of yours |

## `list`

List messages in a folder.

Takes the same filters as trash, move, label and export, so you can preview a selection here before acting on it. A text filter goes through Proton's own index, which lags a change by a few seconds and does not cover bodies, or through the copy `index create mail` builds, which does.

Looks in the inbox unless told otherwise. Use --folder all to search everything.

Newest first, or largest first with --sort size; --desc reverses either.

```
proton mail messages list
```

```bash
proton mail messages list
proton mail messages list --unread
proton mail messages list --folder archive --limit 50
proton mail messages list --starred --output json
proton mail messages list --from billing@example.com --folder all
proton mail messages list --via work@example.com --folder all
proton mail messages list --has-attachments --from billing@example.com --folder all
proton mail messages list --keyword invoice --after 2026-01-01 --folder all
proton mail messages list --keyword 'parking permit' --folder all
proton mail messages list --folder all --sort size --limit 10
```

| Flag | Description |
| --- | --- |
| `--after string` | First day to include (YYYY-MM-DD) |
| `--before string` | Last day to include (YYYY-MM-DD) |
| `--desc` | Reverse the order |
| `--folder string` | Folder or label to look in (default: inbox) |
| `--from string` | Match the sender's address |
| `--has-attachments` | Match messages with attachments |
| `--keyword string` | Match text in the subject, a name or an address, and in bodies once a mail index exists |
| `--limit int` | How many messages per page; 0 for all of them (default `25`) |
| `--newer-than string` | Match messages newer than DURATION |
| `--older-than string` | Match messages older than DURATION (e.g. 30d, 2w, 1h) |
| `--page int` | Which page of results, counting from zero |
| `--read` | Match read messages |
| `--sort string` | Order by: time, size (default `time`) |
| `--starred` | Match starred messages |
| `--subject string` | Match text in the subject |
| `--to string` | Match a recipient's address |
| `--unread` | Match unread messages |
| `--via string` | Match mail that arrived at, or left from, this address of yours |

## `mark`

Set what messages count as.

Holds `legitimate`, `phishing`, `read` and `unread`.

### `mark legitimate`

Mark a message Proton flagged as legitimate.

This overrules the phishing or suspicious verdict on that message alone. To let a sender through from now on, use `settings senders allow`.

```
proton mail messages mark legitimate REF...
```

```bash
proton mail messages mark legitimate 5bH2mQxK
```

### `mark phishing`

Report a message to Proton as phishing.

Proton receives the message decrypted, body included, and the message moves to spam. To keep a sender out without reporting anything, use `settings senders block`.

```
proton mail messages mark phishing REF...
```

```bash
proton mail messages mark phishing 5bH2mQxK
proton mail messages mark phishing 'Your account will be suspended' --dry-run
```

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
| `--after string` | First day to include (YYYY-MM-DD) |
| `--all` | Act on everything in scope, rather than a subset |
| `--before string` | Last day to include (YYYY-MM-DD) |
| `--folder string` | Folder or label to look in (default: all) |
| `--from string` | Match the sender's address |
| `--has-attachments` | Match messages with attachments |
| `--keyword string` | Match text in the subject, a name or an address, and in bodies once a mail index exists |
| `--limit int` | Most messages to affect; 0 for no cap (default `150`) |
| `--newer-than string` | Match messages newer than DURATION |
| `--older-than string` | Match messages older than DURATION (e.g. 30d, 2w, 1h) |
| `--read` | Match read messages |
| `--starred` | Match starred messages |
| `--subject string` | Match text in the subject |
| `--to string` | Match a recipient's address |
| `--unread` | Match unread messages |
| `--via string` | Match mail that arrived at, or left from, this address of yours |

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
| `--after string` | First day to include (YYYY-MM-DD) |
| `--all` | Act on everything in scope, rather than a subset |
| `--before string` | Last day to include (YYYY-MM-DD) |
| `--folder string` | Folder or label to look in (default: all) |
| `--from string` | Match the sender's address |
| `--has-attachments` | Match messages with attachments |
| `--keyword string` | Match text in the subject, a name or an address, and in bodies once a mail index exists |
| `--limit int` | Most messages to affect; 0 for no cap (default `150`) |
| `--newer-than string` | Match messages newer than DURATION |
| `--older-than string` | Match messages older than DURATION (e.g. 30d, 2w, 1h) |
| `--read` | Match read messages |
| `--starred` | Match starred messages |
| `--subject string` | Match text in the subject |
| `--to string` | Match a recipient's address |
| `--unread` | Match unread messages |
| `--via string` | Match mail that arrived at, or left from, this address of yours |

## `move`

Move messages to a folder.

A message is in exactly one folder, so this takes it out of the one it was in. To tag it while leaving it where it is, use `label` instead.

```
proton mail messages move [REF...]
```

```bash
proton mail messages move 'Invoice #2291' --into archive
proton mail messages move --from newsletter@example.com --older-than 90d --into archive
proton mail messages move --folder inbox --read --older-than 30d --into archive
```

| Flag | Description |
| --- | --- |
| `--after string` | First day to include (YYYY-MM-DD) |
| `--all` | Act on everything in scope, rather than a subset |
| `--before string` | Last day to include (YYYY-MM-DD) |
| `--folder string` | Folder or label to look in (default: all) |
| `--from string` | Match the sender's address |
| `--has-attachments` | Match messages with attachments |
| `--into string` | Destination folder, by name or ID |
| `--keyword string` | Match text in the subject, a name or an address, and in bodies once a mail index exists |
| `--limit int` | Most messages to affect; 0 for no cap (default `150`) |
| `--newer-than string` | Match messages newer than DURATION |
| `--older-than string` | Match messages older than DURATION (e.g. 30d, 2w, 1h) |
| `--read` | Match read messages |
| `--starred` | Match starred messages |
| `--subject string` | Match text in the subject |
| `--to string` | Match a recipient's address |
| `--unread` | Match unread messages |
| `--via string` | Match mail that arrived at, or left from, this address of yours |

## `receipt`

Tell the sender you read their message.

Only a message that asked for a read receipt can be answered, and only once. `get` shows such a message as Receipt: requested.

To ask for one yourself, send with `proton mail messages send --request-receipt`.

```
proton mail messages receipt REF...
```

```bash
proton mail messages receipt 'Invoice #2291'
proton mail messages receipt 5bH2mQxK --dry-run
```

## `reply`

Reply to a message.

The original is quoted below your text, the subject gains "Re:", and the reply leaves from the address the original arrived on.

--draft stops before sending, so you can edit it with `mail drafts update`.

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
| `--eo-password-file string` | Read the password for recipients outside Proton from a file, or - for stdin |
| `--eo-password-hint string` | Hint shown to password-protected recipients |
| `--everyone` | Reply to everyone who was on the message, not just the sender |
| `--expires string` | Self-destruct after DURATION (e.g. 7d, 24h), or never |
| `--from string` | Address to send from, by email or ID (default: your primary) |
| `--html` | Compose in HTML (default: match the original) |
| `--no-quote` | Do not quote the original message |
| `--no-signature` | Leave out this address's signature and Proton's footer |
| `--request-receipt` | Ask the recipients to confirm when they read it |
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
proton mail messages send --to jane@example.com --subject Contract --body 'Signed and attached.' --request-receipt
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
| `--eo-password-file string` | Read the password for recipients outside Proton from a file, or - for stdin |
| `--eo-password-hint string` | Hint shown to password-protected recipients |
| `--expires string` | Self-destruct after DURATION (e.g. 7d, 24h), or never |
| `--from string` | Address to send from, by email or ID (default: your primary) |
| `--html` | Treat the body as HTML rather than plain text |
| `--no-signature` | Leave out this address's signature and Proton's footer |
| `--request-receipt` | Ask the recipients to confirm when they read it |
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
| `--after string` | First day to include (YYYY-MM-DD) |
| `--all` | Act on everything in scope, rather than a subset |
| `--before string` | Last day to include (YYYY-MM-DD) |
| `--folder string` | Folder or label to look in (default: all) |
| `--from string` | Match the sender's address |
| `--has-attachments` | Match messages with attachments |
| `--keyword string` | Match text in the subject, a name or an address, and in bodies once a mail index exists |
| `--limit int` | Most messages to affect; 0 for no cap (default `150`) |
| `--newer-than string` | Match messages newer than DURATION |
| `--older-than string` | Match messages older than DURATION (e.g. 30d, 2w, 1h) |
| `--read` | Match read messages |
| `--starred` | Match starred messages |
| `--subject string` | Match text in the subject |
| `--to string` | Match a recipient's address |
| `--unread` | Match unread messages |
| `--via string` | Match mail that arrived at, or left from, this address of yours |

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
| `--after string` | First day to include (YYYY-MM-DD) |
| `--all` | Act on everything in scope, rather than a subset |
| `--before string` | Last day to include (YYYY-MM-DD) |
| `--folder string` | Folder or label to look in (default: all) |
| `--from string` | Match the sender's address |
| `--has-attachments` | Match messages with attachments |
| `--keyword string` | Match text in the subject, a name or an address, and in bodies once a mail index exists |
| `--limit int` | Most messages to affect; 0 for no cap (default `150`) |
| `--newer-than string` | Match messages newer than DURATION |
| `--older-than string` | Match messages older than DURATION (e.g. 30d, 2w, 1h) |
| `--read` | Match read messages |
| `--starred` | Match starred messages |
| `--subject string` | Match text in the subject |
| `--to string` | Match a recipient's address |
| `--unread` | Match unread messages |
| `--via string` | Match mail that arrived at, or left from, this address of yours |

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
| `--after string` | First day to include (YYYY-MM-DD) |
| `--all` | Act on everything in scope, rather than a subset |
| `--before string` | Last day to include (YYYY-MM-DD) |
| `--folder string` | Folder or label to look in (default: all) |
| `--from string` | Match the sender's address |
| `--has-attachments` | Match messages with attachments |
| `--keyword string` | Match text in the subject, a name or an address, and in bodies once a mail index exists |
| `--label string` | The label to attach or detach, by name or ID |
| `--limit int` | Most messages to affect; 0 for no cap (default `150`) |
| `--newer-than string` | Match messages newer than DURATION |
| `--older-than string` | Match messages older than DURATION (e.g. 30d, 2w, 1h) |
| `--read` | Match read messages |
| `--starred` | Match starred messages |
| `--subject string` | Match text in the subject |
| `--to string` | Match a recipient's address |
| `--unread` | Match unread messages |
| `--via string` | Match mail that arrived at, or left from, this address of yours |

## `unschedule`

Cancel a scheduled send.

The message leaves the queue and returns to Drafts, keeping its ID. To change the time, cancel it and send again with --send-at.

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
| `--after string` | First day to include (YYYY-MM-DD) |
| `--all` | Act on everything in scope, rather than a subset |
| `--before string` | Last day to include (YYYY-MM-DD) |
| `--folder string` | Folder or label to look in (default: all) |
| `--from string` | Match the sender's address |
| `--has-attachments` | Match messages with attachments |
| `--keyword string` | Match text in the subject, a name or an address, and in bodies once a mail index exists |
| `--limit int` | Most messages to affect; 0 for no cap (default `150`) |
| `--newer-than string` | Match messages newer than DURATION |
| `--older-than string` | Match messages older than DURATION (e.g. 30d, 2w, 1h) |
| `--read` | Match read messages |
| `--starred` | Match starred messages |
| `--subject string` | Match text in the subject |
| `--to string` | Match a recipient's address |
| `--unread` | Match unread messages |
| `--via string` | Match mail that arrived at, or left from, this address of yours |

## `unsubscribe`

Ask the mailing list a message came from to stop.

A list offers one of three ways, and the answer says which was used. Proton submits a one-click form on your behalf; an unsubscribe address is a message sent from the address the list writes to; a link is a page, which is opened in your browser and printed either way.

To go by sender instead of by message, see `proton mail mailing-lists`.

```
proton mail messages unsubscribe REF...
```

```bash
proton mail messages unsubscribe 5bH2mQxK
```

## `update`

Change when messages delete themselves.

--expires takes a duration, or never to stop them expiring. A message already counting down reports the moment it expires rather than how long is left.

```
proton mail messages update [REF...]
```

```bash
proton mail messages update 5bH2mQxK --expires 7d
proton mail messages update --from newsletter@example.com --expires 30d
proton mail messages update 5bH2mQxK --expires never
```

| Flag | Description |
| --- | --- |
| `--after string` | First day to include (YYYY-MM-DD) |
| `--all` | Act on everything in scope, rather than a subset |
| `--before string` | Last day to include (YYYY-MM-DD) |
| `--expires string` | Delete them after DURATION (e.g. 7d, 24h), or never |
| `--folder string` | Folder or label to look in (default: all) |
| `--from string` | Match the sender's address |
| `--has-attachments` | Match messages with attachments |
| `--keyword string` | Match text in the subject, a name or an address, and in bodies once a mail index exists |
| `--limit int` | Most messages to affect; 0 for no cap (default `150`) |
| `--newer-than string` | Match messages newer than DURATION |
| `--older-than string` | Match messages older than DURATION (e.g. 30d, 2w, 1h) |
| `--password-file string` | Read the account password from a file, or - for stdin |
| `--read` | Match read messages |
| `--starred` | Match starred messages |
| `--subject string` | Match text in the subject |
| `--to string` | Match a recipient's address |
| `--totp string` | Two-factor code |
| `--unread` | Match unread messages |
| `--via string` | Match mail that arrived at, or left from, this address of yours |

## `watch`

Print each message as it arrives, until you stop it.

It reports what happens while it is watching, so nothing that arrived beforehand comes up. A thread returning from snooze counts as arriving.

Without --folder it covers what Proton notifies you about: the inbox, starred mail, and every folder whose notifications are on, which `settings folders list` shows under NOTIFY. With categories on, the inbox counts only in the categories that notify and the hidden ones, whose mail shows under Primary - `settings categories list` shows which.

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
