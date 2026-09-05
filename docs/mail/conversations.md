# proton mail conversations

Whole threads.

Every command under `proton mail conversations`, with the arguments and flags it takes. For these commands in use, see [the mail guide](README.md).

Holds `attachments`, `delete`, `export`, `forward`, `get`, `label`, `list`, `mark`, `move`, `reply`, `snooze`, `star`, `trash`, `unlabel`, `unsnooze` and `unstar`.

## `attachments`

Files attached anywhere in a thread.

Holds `download` and `list`.

### `attachments download`

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

### `attachments list`

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

## `delete`

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
| `--limit int` | Most messages to affect; 0 for no cap (default `150`) |
| `--newer-than string` | Match messages newer than DURATION |
| `--older-than string` | Match messages older than DURATION (e.g. 30d, 2w, 1h) |
| `--starred` | Match starred messages |
| `--subject string` | Match text in the subject |
| `--to string` | Match a recipient's address |
| `--unread` | Match unread messages |

## `export`

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

## `forward`

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

## `get`

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

## `label`

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
| `--limit int` | Most messages to affect; 0 for no cap (default `150`) |
| `--newer-than string` | Match messages newer than DURATION |
| `--older-than string` | Match messages older than DURATION (e.g. 30d, 2w, 1h) |
| `--starred` | Match starred messages |
| `--subject string` | Match text in the subject |
| `--to string` | Match a recipient's address |
| `--unread` | Match unread messages |

## `list`

List threads in a folder.

Takes the same filters as the verbs that organise threads, so you can preview a selection here before acting on it. Text filters go through Proton's index, which lags a change by a few seconds.

Looks in the inbox unless told otherwise. Use --folder all to search everything.

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
| `--page-size int` | How many threads per page; 0 for all of them (default `25`) |
| `--starred` | Match starred messages |
| `--subject string` | Match text in the subject |
| `--to string` | Match a recipient's address |
| `--unread` | Match unread messages |

## `mark`

Set whether threads count as read.

Holds `read` and `unread`.

### `mark read`

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
| `--limit int` | Most messages to affect; 0 for no cap (default `150`) |
| `--newer-than string` | Match messages newer than DURATION |
| `--older-than string` | Match messages older than DURATION (e.g. 30d, 2w, 1h) |
| `--starred` | Match starred messages |
| `--subject string` | Match text in the subject |
| `--to string` | Match a recipient's address |
| `--unread` | Match unread messages |

### `mark unread`

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
| `--limit int` | Most messages to affect; 0 for no cap (default `150`) |
| `--newer-than string` | Match messages newer than DURATION |
| `--older-than string` | Match messages older than DURATION (e.g. 30d, 2w, 1h) |
| `--starred` | Match starred messages |
| `--subject string` | Match text in the subject |
| `--to string` | Match a recipient's address |
| `--unread` | Match unread messages |

## `move`

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
| `--limit int` | Most messages to affect; 0 for no cap (default `150`) |
| `--newer-than string` | Match messages newer than DURATION |
| `--older-than string` | Match messages older than DURATION (e.g. 30d, 2w, 1h) |
| `--starred` | Match starred messages |
| `--subject string` | Match text in the subject |
| `--to string` | Match a recipient's address |
| `--unread` | Match unread messages |

## `reply`

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

## `snooze`

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
| `--limit int` | Most messages to affect; 0 for no cap (default `150`) |
| `--newer-than string` | Match messages newer than DURATION |
| `--older-than string` | Match messages older than DURATION (e.g. 30d, 2w, 1h) |
| `--starred` | Match starred messages |
| `--subject string` | Match text in the subject |
| `--to string` | Match a recipient's address |
| `--unread` | Match unread messages |
| `--until string` | When they come back (e.g. 3d, or 2026-04-17T09:00) |

## `star`

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
| `--limit int` | Most messages to affect; 0 for no cap (default `150`) |
| `--newer-than string` | Match messages newer than DURATION |
| `--older-than string` | Match messages older than DURATION (e.g. 30d, 2w, 1h) |
| `--starred` | Match starred messages |
| `--subject string` | Match text in the subject |
| `--to string` | Match a recipient's address |
| `--unread` | Match unread messages |

## `trash`

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
| `--limit int` | Most messages to affect; 0 for no cap (default `150`) |
| `--newer-than string` | Match messages newer than DURATION |
| `--older-than string` | Match messages older than DURATION (e.g. 30d, 2w, 1h) |
| `--starred` | Match starred messages |
| `--subject string` | Match text in the subject |
| `--to string` | Match a recipient's address |
| `--unread` | Match unread messages |

## `unlabel`

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
| `--limit int` | Most messages to affect; 0 for no cap (default `150`) |
| `--newer-than string` | Match messages newer than DURATION |
| `--older-than string` | Match messages older than DURATION (e.g. 30d, 2w, 1h) |
| `--starred` | Match starred messages |
| `--subject string` | Match text in the subject |
| `--to string` | Match a recipient's address |
| `--unread` | Match unread messages |

## `unsnooze`

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
| `--limit int` | Most messages to affect; 0 for no cap (default `150`) |
| `--newer-than string` | Match messages newer than DURATION |
| `--older-than string` | Match messages older than DURATION (e.g. 30d, 2w, 1h) |
| `--starred` | Match starred messages |
| `--subject string` | Match text in the subject |
| `--to string` | Match a recipient's address |
| `--unread` | Match unread messages |

## `unstar`

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
| `--limit int` | Most messages to affect; 0 for no cap (default `150`) |
| `--newer-than string` | Match messages newer than DURATION |
| `--older-than string` | Match messages older than DURATION (e.g. 30d, 2w, 1h) |
| `--starred` | Match starred messages |
| `--subject string` | Match text in the subject |
| `--to string` | Match a recipient's address |
| `--unread` | Match unread messages |

---

Every command also takes the [flags that work everywhere](../about/commands.md#flags-that-work-on-every-command).
