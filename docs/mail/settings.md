# proton mail settings

How Mail behaves.

Every command under `proton mail settings`, with the arguments and flags it takes. For these commands in use, see [the mail guide](README.md).

Holds `addresses`, `autoreply`, `filters`, `folders`, `forwarding`, `get`, `labels`, `list`, `senders` and `set`.

## `addresses`

Your addresses, display names and signatures.

Holds `get`, `list` and `update`.

### `addresses get`

Show one address, including its signature.

```
proton mail settings addresses get REF
```

```bash
proton mail settings addresses get me@proton.me
```

### `addresses list`

List the addresses on the account.

```
proton mail settings addresses list
```

```bash
proton mail settings addresses list
```

### `addresses update`

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

## `autoreply`

The automatic reply and its schedule.

Holds `disable`, `enable`, `get` and `set`.

### `autoreply disable`

Turn the auto-reply off, keeping its schedule.

```
proton mail settings autoreply disable
```

```bash
proton mail settings autoreply disable
```

| Flag | Description |
| --- | --- |
| `--password-file string` | Read the account password from a file |
| `--password-stdin` | Read the account password from stdin |
| `--totp string` | Two-factor code |

### `autoreply enable`

Turn the auto-reply on, keeping its schedule.

```
proton mail settings autoreply enable
```

```bash
proton mail settings autoreply enable
```

| Flag | Description |
| --- | --- |
| `--password-file string` | Read the account password from a file |
| `--password-stdin` | Read the account password from stdin |
| `--totp string` | Two-factor code |

### `autoreply get`

Show the auto-reply and its schedule.

```
proton mail settings autoreply get
```

```bash
proton mail settings autoreply get
```

### `autoreply set`

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

## `filters`

Server-side Sieve filters.

Holds `apply`, `create`, `delete`, `disable`, `enable`, `get`, `list`, `reorder` and `update`.

### `filters apply`

Run filters over mail that is already in the mailbox.

A filter normally runs once, as mail arrives, so a rule written today does nothing about yesterday's mail.

With no filter named, every enabled filter runs.

```
proton mail settings filters apply [REF...]
```

```bash
proton mail settings filters apply
proton mail settings filters apply Newsletters
```

### `filters create`

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

### `filters delete`

Delete filters.

```
proton mail settings filters delete REF...
```

```bash
proton mail settings filters delete Receipts
```

### `filters disable`

Disable filters.

```
proton mail settings filters disable REF...
```

```bash
proton mail settings filters disable Receipts
```

### `filters enable`

Enable filters.

```
proton mail settings filters enable REF...
```

```bash
proton mail settings filters enable Receipts
```

### `filters get`

Show what a filter matches and does.

```
proton mail settings filters get REF
```

```bash
proton mail settings filters get Receipts
```

### `filters list`

List your filters.

```
proton mail settings filters list
```

```bash
proton mail settings filters list
```

### `filters reorder`

Set the order filters run in.

The first rule to file a message wins, so the order decides where mail lands. Name every filter, in the order you want them. This replaces the whole order; a partial one is refused.

```
proton mail settings filters reorder REF...
```

```bash
proton mail settings filters reorder Newsletters Receipts Archive
```

### `filters update`

Change what a filter is called, matches, or does.

--if and the actions beside it replace the whole rule rather than adding to it. The filter keeps its place in the order, and stays enabled or disabled as it was.

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

## `folders`

Folders, which a message lives in.

Holds `create`, `delete`, `list` and `update`.

### `folders create`

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

### `folders delete`

Delete folders.

```
proton mail settings folders delete REF...
```

```bash
proton mail settings folders delete Receipts
```

### `folders list`

List your folders.

```
proton mail settings folders list
```

```bash
proton mail settings folders list
```

### `folders update`

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
| `--color string` | New accent color, by name (purple) or hex (#8080FF) |
| `--name string` | New name |
| `--notify` | Tell you when mail arrives here (default `true`) |
| `--parent string` | Move it inside this folder, by ID |

## `forwarding`

Mail forwarded to and from your addresses.

Holds `create`, `delete`, `disable`, `enable`, `get`, `list` and `resend`.

### `forwarding create`

Forward one of your addresses to another Proton address.

REF is the address of yours mail arrives at, EMAIL is the Proton address it is handed to. Mail stays end-to-end encrypted: a key is derived from your address key so Proton can re-wrap each message for them without reading it.

Nothing is forwarded until they accept, which they do in a Proton client - accepting one writes a new address key, and proton changes no key material.

Forwarding to an address outside Proton is not built: Proton emails it a link its owner must follow, which no command can answer.

```
proton mail settings forwarding create REF EMAIL
```

```bash
proton mail settings forwarding create me@proton.me jane@proton.me
```

### `forwarding delete`

Stop forwardings, in either direction.

```
proton mail settings forwarding delete REF...
```

```bash
proton mail settings forwarding delete jane@proton.me
```

### `forwarding disable`

Pause forwardings without taking them down.

```
proton mail settings forwarding disable REF...
```

```bash
proton mail settings forwarding disable jane@proton.me
```

### `forwarding enable`

Resume paused forwardings.

```
proton mail settings forwarding enable REF...
```

```bash
proton mail settings forwarding enable jane@proton.me
```

### `forwarding get`

Show one forwarding.

```
proton mail settings forwarding get REF
```

```bash
proton mail settings forwarding get jane@proton.me
```

### `forwarding list`

List forwardings in both directions.

Outgoing is mail leaving one of your addresses for somebody else's; incoming is mail somebody else is sending to you. A forwarding is pending until the forwardee accepts it, and outdated once the forwarder's key changes.

```
proton mail settings forwarding list
```

```bash
proton mail settings forwarding list
```

### `forwarding resend`

Ask the forwardee again.

```
proton mail settings forwarding resend REF...
```

```bash
proton mail settings forwarding resend jane@proton.me
```

## `get`

Show the mail settings now in effect.

```
proton mail settings get
```

```bash
proton mail settings get
```

## `labels`

Labels, which a message carries.

Holds `create`, `delete`, `list` and `update`.

### `labels create`

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

### `labels delete`

Delete labels.

```
proton mail settings labels delete REF...
```

```bash
proton mail settings labels delete Work
```

### `labels list`

List your labels.

```
proton mail settings labels list
```

```bash
proton mail settings labels list
```

### `labels update`

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
| `--color string` | New accent color, by name (purple) or hex (#8080FF) |
| `--name string` | New name |

## `list`

List the mail settings that can be changed.

```
proton mail settings list
```

```bash
proton mail settings list
```

## `senders`

Who always reaches the inbox, and who never does.

Holds `allow`, `block`, `forget`, `list` and `spam`.

### `senders allow`

Always let someone reach the inbox.

A whole domain works too, written with the @: `@example.com`.

Deciding again about the same sender replaces the earlier decision rather than colliding with it.

```
proton mail settings senders allow EMAIL...
```

```bash
proton mail settings senders allow billing@example.com
```

### `senders block`

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

### `senders forget`

Drop a standing decision, letting the spam filter decide again.

```
proton mail settings senders forget EMAIL...
```

```bash
proton mail settings senders forget billing@example.com
```

### `senders list`

List every standing decision about a sender.

```
proton mail settings senders list
```

```bash
proton mail settings senders list
```

### `senders spam`

Send someone's mail straight to spam.

A whole domain works too, written with the @: `@example.com`.

Deciding again about the same sender replaces the earlier decision rather than colliding with it.

```
proton mail settings senders spam EMAIL...
```

```bash
proton mail settings senders spam newsletter@example.com
```

## `set`

Change one mail setting.

```
proton mail settings set KEY VALUE
```

```bash
proton mail settings set signature off
proton mail settings set view-mode conversation
```

---

Every command also takes the [flags that work everywhere](../about/commands.md#flags-that-work-on-every-command).
