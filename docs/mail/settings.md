# proton mail settings

How Mail behaves.

Every command under `proton mail settings`, with the arguments and flags it takes. For these commands in use, see [the mail guide](README.md).

Holds `addresses`, `autoreply`, `categories`, `domains`, `filters`, `folders`, `forwarding`, `get`, `imports`, `labels`, `list`, `senders`, `set` and `smtp-tokens`.

## `addresses`

Your addresses, display names and signatures.

An address sends and receives mail under its own name. Adding, disabling and deleting one needs a paid Mail plan. Your first Proton address and your short-domain address stay enabled and cannot be deleted.

Holds `create`, `delete`, `disable`, `enable`, `get`, `list`, `reorder` and `update`.

### `addresses create`

Add an address to the account.

EMAIL is the address to add. Its domain has to be one the account can use: a Proton domain, or a custom domain already set up. Adding an address needs a paid Mail plan.

Your short-domain address is your username at pm.me. It takes the signature of your default address, and its display name unless you pass --display-name. Once it exists it cannot be disabled or deleted.

The address sends and receives as soon as it exists. An account that creates post-quantum keys is refused: add the address in a Proton client.

Asks for your password even when you are signed in. With no terminal to ask, pass --password-file, which takes - for stdin.

```
proton mail settings addresses create EMAIL
```

```bash
proton mail settings addresses create work@example.com
proton mail settings addresses create work@example.com --display-name Work
proton mail settings addresses create alice@pm.me
```

| Flag | Description |
| --- | --- |
| `--display-name string` | Name recipients see next to the address |
| `--password-file string` | Read the account password from a file, or - for stdin |
| `--totp string` | Two-factor code |

### `addresses delete`

Delete addresses.

REF is an address of yours. Proton allows one address deletion a year unless the address is on a custom domain. Your first Proton address, your short-domain address and the account's default address cannot be deleted.

A deleted address cannot be used again, by you or by anybody else.

Asks for your password even when you are signed in. With no terminal to ask, pass --password-file, which takes - for stdin.

```
proton mail settings addresses delete REF...
```

```bash
proton mail settings addresses delete work@example.com
```

| Flag | Description |
| --- | --- |
| `--password-file string` | Read the account password from a file, or - for stdin |
| `--totp string` | Two-factor code |

### `addresses disable`

Stop an address sending and receiving.

REF is an address of yours that is enabled. Everything it already holds stays, and enabling it again needs nothing else.

Asks for your password even when you are signed in. With no terminal to ask, pass --password-file, which takes - for stdin.

```
proton mail settings addresses disable REF...
```

```bash
proton mail settings addresses disable work@example.com
```

| Flag | Description |
| --- | --- |
| `--password-file string` | Read the account password from a file, or - for stdin |
| `--totp string` | Two-factor code |

### `addresses enable`

Let a disabled address send and receive again.

REF is an address of yours that is disabled.

Asks for your password even when you are signed in. With no terminal to ask, pass --password-file, which takes - for stdin.

```
proton mail settings addresses enable REF...
```

```bash
proton mail settings addresses enable work@example.com
```

| Flag | Description |
| --- | --- |
| `--password-file string` | Read the account password from a file, or - for stdin |
| `--totp string` | Two-factor code |

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

| Flag | Description |
| --- | --- |
| `--limit int` | How many addresses per page; 0 for all of them |
| `--page int` | Which page of results, counting from zero |

### `addresses reorder`

Make an address the default, or set their order.

The first address is the default: mail leaves from it when no --from is given. Name the addresses that should come first, in order; the rest keep the order they are in.

A disabled or external address, or one that cannot send or receive, cannot be the default.

```
proton mail settings addresses reorder REF...
```

```bash
proton mail settings addresses reorder alice@pm.me
proton mail settings addresses reorder alice@pm.me work@example.com alice@proton.me
```

### `addresses update`

Set the display name recipients see and the signature appended to mail sent from this address.

A signature is stored as HTML. Plain text is escaped and its newlines become line breaks; --html passes markup through untouched.

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
| `--password-file string` | Read the account password from a file, or - for stdin |
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
| `--password-file string` | Read the account password from a file, or - for stdin |
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
proton mail settings autoreply set --repeat permanent --body 'Away until Monday.'
proton mail settings autoreply set --body 'On holiday.' --start 2026-07-01T09:00 --end 2026-07-14T17:00
```

| Flag | Description |
| --- | --- |
| `--body string` | The reply to send (- reads stdin) |
| `--days stringSlice` | Days it is active, for a daily schedule, e.g. mon,tue,wed |
| `--end string` | End of the window (grammar depends on --repeat) |
| `--html` | Treat the body as HTML rather than escaping it |
| `--password-file string` | Read the account password from a file, or - for stdin |
| `--repeat string` | How the schedule repeats: fixed, daily, weekly, monthly, permanent (default `fixed`) |
| `--start string` | Start of the window (grammar depends on --repeat) |
| `--totp string` | Two-factor code |

## `categories`

The category tabs the inbox is sorted into.

Primary is always shown, and its notifications cannot be changed. In the web it also holds the mail of every hidden category. Changing the others needs categories on: `proton mail settings set category-view on`.

Holds `disable`, `enable`, `list` and `update`.

### `categories disable`

Hide categories from the inbox.

One category besides Primary always stays shown. To stop using categories, run `proton mail settings set category-view off`.

```
proton mail settings categories disable REF...
```

```bash
proton mail settings categories disable promotions
```

### `categories enable`

Show categories as tabs in the inbox.

```
proton mail settings categories enable REF...
```

```bash
proton mail settings categories enable transactions
proton mail settings categories enable transactions updates
```

### `categories list`

List the categories, and which show and notify.

```
proton mail settings categories list
```

```bash
proton mail settings categories list
```

| Flag | Description |
| --- | --- |
| `--limit int` | How many categories per page; 0 for all of them |
| `--page int` | Which page of results, counting from zero |

### `categories update`

Change whether a category notifies.

A hidden category has no notifications to change: enable it first.

```
proton mail settings categories update REF
```

```bash
proton mail settings categories update social --notify
proton mail settings categories update social --notify=false
```

| Flag | Description |
| --- | --- |
| `--notify` | Tell you when mail arrives here (default `true`) |

## `domains`

Custom domains, and where stray mail goes.

A custom domain sends and receives under your own name. Adding one needs a paid Mail plan, and how many you may have depends on it.

A domain carries no mail until its DNS entries are in place. `get` shows them and says which ones Proton can see.

Holds `create`, `delete`, `get`, `list` and `update`.

### `domains create`

Add a custom domain to the account.

DOMAIN is a domain you own, written as example.com. Adding one needs a paid Mail plan.

The domain carries no mail until its verification entry is in your DNS, and no address can be added on it until Proton has seen that entry. `get` shows every entry the domain needs.

Asks for your password even when you are signed in. With no terminal to ask, pass --password-file, which takes - for stdin.

```
proton mail settings domains create DOMAIN
```

```bash
proton mail settings domains create example.com
```

| Flag | Description |
| --- | --- |
| `--password-file string` | Read the account password from a file, or - for stdin |
| `--totp string` | Two-factor code |

### `domains delete`

Remove a custom domain.

Every address on the domain stops sending and receiving, and the mail they hold stays. Adding the domain again needs its DNS entries verified from scratch.

Asks for your password even when you are signed in. With no terminal to ask, pass --password-file, which takes - for stdin.

```
proton mail settings domains delete REF
```

```bash
proton mail settings domains delete example.com
```

| Flag | Description |
| --- | --- |
| `--password-file string` | Read the account password from a file, or - for stdin |
| `--totp string` | Two-factor code |

### `domains get`

Show a domain's DNS entries and their status.

Reads the domain's DNS again each time it runs, so what it reports is what Proton can see now.

Each check reads ok, missing, wrong, duplicate, wrong-priority, backup, error, warning, delegated or relaxed. Every entry has to stay in place: removing the verification entry gives the domain up.

```
proton mail settings domains get REF
```

```bash
proton mail settings domains get example.com
```

### `domains list`

List the custom domains on the account.

STATUS is active, unverified or warning. DNS is ok, or the checks that are not passing. Both come from the last check Proton made; `get` checks again.

```
proton mail settings domains list
```

```bash
proton mail settings domains list
```

| Flag | Description |
| --- | --- |
| `--desc` | Reverse the order |
| `--limit int` | How many domains per page; 0 for all of them |
| `--page int` | Which page of results, counting from zero |
| `--sort string` | Order by: domain (default `domain`) |

### `domains update`

Set the address that catches stray mail.

Mail sent to a name that does not exist at the domain arrives at the catch-all address instead of being refused.

--catch-all takes one of your addresses on that domain. One address catches at a time, so naming another moves it, and `--catch-all none` turns it off.

Asks for your password even when you are signed in. With no terminal to ask, pass --password-file, which takes - for stdin.

```
proton mail settings domains update REF
```

```bash
proton mail settings domains update example.com --catch-all work@example.com
proton mail settings domains update example.com --catch-all none
```

| Flag | Description |
| --- | --- |
| `--catch-all string` | Address that takes mail sent to a name the domain has not got, or none |
| `--password-file string` | Read the account password from a file, or - for stdin |
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
| `--into string` | Move matching mail into this folder (archive, inbox, spam, trash, or one of yours) |
| `--label stringArray` | Apply this label to matching mail (repeatable) |
| `--mark-read` | Mark matching mail as read |
| `--match string` | Whether every condition must hold, or any one of them: all, any (default `all`) |
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

| Flag | Description |
| --- | --- |
| `--limit int` | How many filters per page; 0 for all of them |
| `--page int` | Which page of results, counting from zero |

### `filters reorder`

Set the order filters run in.

The first rule to file a message wins, so the order decides where mail lands. Name the filters that should run first, in order; the rest keep the order they are in.

```
proton mail settings filters reorder REF...
```

```bash
proton mail settings filters reorder Receipts
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
| `--into string` | Move matching mail into this folder (archive, inbox, spam, trash, or one of yours) |
| `--label stringArray` | Apply this label to matching mail (repeatable) |
| `--mark-read` | Mark matching mail as read |
| `--match string` | Whether every condition must hold, or any one of them: all, any (default `all`) |
| `--name string` | New name |
| `--sieve string` | New Sieve script (- reads stdin) |
| `--star` | Star matching mail |

## `folders`

Folders, which a message lives in.

Holds `create`, `delete`, `list`, `reorder` and `update`.

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
| `--parent string` | Put it inside this folder |

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

| Flag | Description |
| --- | --- |
| `--limit int` | How many folders per page; 0 for all of them |
| `--page int` | Which page of results, counting from zero |

### `folders reorder`

Set the order your folders are kept in.

Name the folders that should come first, in order; the rest keep the order they are in. --alphabetical sorts them all instead.

A folder is ordered among the folders it sits beside, so name folders from one place at a time. --alphabetical covers every level.

```
proton mail settings folders reorder [REF...]
```

```bash
proton mail settings folders reorder Receipts
proton mail settings folders reorder --alphabetical
```

| Flag | Description |
| --- | --- |
| `--alphabetical` | Sort them all alphabetically instead of naming any |

### `folders update`

Rename a folder, recolor it, or move it.

--parent takes the folder to put it inside, or none to bring it back to the top level. --notify says whether mail landing here is worth telling you about.

```
proton mail settings folders update REF
```

```bash
proton mail settings folders update Receipts --name Invoices
proton mail settings folders update 2026 --parent none
proton mail settings folders update Receipts --notify
```

| Flag | Description |
| --- | --- |
| `--color string` | New accent color, by name (purple) or hex (#8080FF) |
| `--name string` | New name |
| `--notify` | Tell you when mail arrives here (default `true`) |
| `--parent string` | Move it inside this folder, or none for the top level |

## `forwarding`

Mail forwarded to and from your addresses.

Outgoing is mail leaving one of your addresses for somebody else's; incoming is mail somebody else sends to you. A forwarding is named by the other party's address.

Holds `accept`, `create`, `decline`, `delete`, `disable`, `enable`, `get`, `list` and `resend`.

### `forwarding accept`

Accept forwardings sent to you.

REF is the forwarder's address, or the forwarding's ID. Only a pending forwarding to one of your addresses can be accepted.

Asks for your password even when you are signed in. With no terminal to ask, pass --password-file, which takes - for stdin.

```
proton mail settings forwarding accept REF...
```

```bash
proton mail settings forwarding accept jane@proton.me
```

| Flag | Description |
| --- | --- |
| `--password-file string` | Read the account password from a file, or - for stdin |
| `--totp string` | Two-factor code |

### `forwarding create`

Forward one of your addresses to another address.

REF is the address of yours mail arrives at, EMAIL is where it is handed to. Needs a paid Mail plan, and nothing is forwarded until they accept it: a Proton address in its Proton client or with `forwarding accept`, an address outside Proton by following the link Proton emails it.

To a Proton address, mail stays end-to-end encrypted. To an address outside Proton, end-to-end encryption for REF is turned off until the last such forwarding from it is deleted, and your password is asked for to turn it off. With no terminal to ask, pass --password-file, which takes - for stdin.

```
proton mail settings forwarding create REF EMAIL
```

```bash
proton mail settings forwarding create me@proton.me jane@proton.me
proton mail settings forwarding create me@proton.me jane@example.com
```

| Flag | Description |
| --- | --- |
| `--password-file string` | Read the account password from a file, or - for stdin |
| `--totp string` | Two-factor code |

### `forwarding decline`

Decline forwardings sent to you.

REF is the forwarder's address, or the forwarding's ID. Only a pending forwarding to one of your addresses can be declined; the forwarder sees it as rejected.

```
proton mail settings forwarding decline REF...
```

```bash
proton mail settings forwarding decline jane@proton.me
```

### `forwarding delete`

Stop forwardings, in either direction.

REF is a forwarding in either direction. Taking down the last forwarding from one of your addresses to an address outside Proton turns end-to-end encryption for it back on, and asks for your password to do it. With no terminal to ask, pass --password-file, which takes - for stdin.

```
proton mail settings forwarding delete REF...
```

```bash
proton mail settings forwarding delete jane@proton.me
```

| Flag | Description |
| --- | --- |
| `--password-file string` | Read the account password from a file, or - for stdin |
| `--totp string` | Two-factor code |

### `forwarding disable`

Pause forwardings without taking them down.

REF is a forwarding you set up that is active. Mail stops being forwarded, and resuming it needs nothing from the forwardee.

```
proton mail settings forwarding disable REF...
```

```bash
proton mail settings forwarding disable jane@proton.me
```

### `forwarding enable`

Resume paused forwardings.

REF is a forwarding you set up that is paused.

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

A forwarding is pending until the forwardee accepts it, paused while the forwarder has it disabled, outdated once the forwarder's key changes, and rejected once the forwardee declines it.

```
proton mail settings forwarding list
```

```bash
proton mail settings forwarding list
```

| Flag | Description |
| --- | --- |
| `--desc` | Reverse the order |
| `--limit int` | How many forwardings per page; 0 for all of them |
| `--page int` | Which page of results, counting from zero |
| `--sort string` | Order by: created, address (default `created`) |

### `forwarding resend`

Ask the forwardee again.

REF is a forwarding you set up that the forwardee declined, or that is outdated. They are offered it again, and it is pending until they accept. A forwarding to an address outside Proton gets its confirmation email sent again instead.

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

## `imports`

Mail brought in from another provider.

An import carries on after the command returns, for as long as it takes. `list` and `get` say how far it has got.

Every import on the account is listed, including one of a Google or Microsoft account. Starting one of those needs a browser, so start it in Proton's own settings and follow it here.

Holds `cancel`, `create`, `delete`, `get`, `list`, `resume` and `undo`.

### `imports cancel`

Stop an import that is running.

What it has already brought over stays where it landed, and the import stays on the list as a record of what it did. `undo` removes the mail.

An import reads as cancelling until it has stopped.

```
proton mail settings imports cancel REF...
```

```bash
proton mail settings imports cancel jane@fastmail.com
```

### `imports create`

Bring another mailbox in over IMAP.

EMAIL is the mailbox to import from, and --imap-password-file is what opens it. That password is sent to Proton, which connects to the mailbox and keeps the password until the import is over. Most providers want an app password here rather than the one you sign in with.

--server and --port are looked up from the address, and have to be given for a provider that is not known.

Every folder is imported unless --skip leaves it out, which leaves out what is inside it too. A folder that matches one of yours lands in it and the rest keep their own names; Gmail's folders arrive as labels. Mail outside --after and --before is left behind, by the day it arrived.

Everything that arrives carries one label, named after the other mailbox unless --label says otherwise.

The import carries on after this returns. `get` says how far it has got, and `undo` takes back everything it brought in.

```
proton mail settings imports create EMAIL
```

```bash
proton mail settings imports create jane@fastmail.com --imap-password-file /run/secrets/fastmail
proton mail settings imports create jane@fastmail.com --imap-password-file - --to jane@proton.me
proton mail settings imports create jane@fastmail.com --imap-password-file /run/secrets/fastmail --after 2024-01-01 --label Fastmail
proton mail settings imports create jane@fastmail.com --imap-password-file /run/secrets/fastmail --skip Spam --skip 'Archive/*'
proton mail settings imports create jane@example.com --imap-password-file /run/secrets/mailbox --server imap.example.com --port 993
```

| Flag | Description |
| --- | --- |
| `--after string` | First day to include (YYYY-MM-DD) |
| `--allow-self-signed` | Accept a certificate Proton cannot verify |
| `--before string` | Last day to include (YYYY-MM-DD) |
| `--imap-password-file string` | Read the other mailbox's password from a file, or - for stdin |
| `--label string` | Label to put on everything that arrives |
| `--port int` | Port the IMAP server answers on |
| `--server string` | IMAP server of the other mailbox |
| `--skip stringArray` | Folder of the other mailbox to leave out, as a name or a glob (repeatable) |
| `--to string` | Your address the imported mail belongs to |

### `imports delete`

Forget the record of a finished import.

The mail it brought over stays where it is. What goes is the row: how much arrived, when, and the offer to undo it.

```
proton mail settings imports delete REF...
```

```bash
proton mail settings imports delete jane@fastmail.com
```

### `imports get`

Show one import, folder by folder.

FOLDERS is each folder of the other mailbox, where its mail lands here, and how much of it has.

An import that stopped says why. A delayed one is waiting on the other provider and picks itself up; a paused one needs `resume`.

```
proton mail settings imports get REF
```

```bash
proton mail settings imports get jane@fastmail.com
```

### `imports list`

List the imports on the account, newest first.

MESSAGES counts what has arrived, against how many there are to fetch. SIZE is filled in once an import is over. VIA is how the other mailbox is reached: imap, google or outlook.

```
proton mail settings imports list
```

```bash
proton mail settings imports list
```

| Flag | Description |
| --- | --- |
| `--desc` | Reverse the order |
| `--limit int` | How many imports per page; 0 for all of them |
| `--page int` | Which page of results, counting from zero |
| `--sort string` | Order by: date, account, state, size (default `date`) |

### `imports resume`

Set a stopped import going again, from where it got to.

One that stopped because the other mailbox refused the connection needs the password again, as --imap-password-file. One that stopped because this account is nearly full needs room made first.

A delayed import needs nothing: it picks itself up.

```
proton mail settings imports resume REF
```

```bash
proton mail settings imports resume jane@fastmail.com
proton mail settings imports resume jane@fastmail.com --imap-password-file /run/secrets/fastmail
```

| Flag | Description |
| --- | --- |
| `--imap-password-file string` | Read the other mailbox's password from a file, or - for stdin |

### `imports undo`

Take back everything an import brought in.

The messages it imported go, and so do the folders and labels it made for them. Nothing brings them back: the mail is still in the mailbox it came from, and fetching it again is a new import.

Only a finished import can be taken back, and only for as long as it is offered.

```
proton mail settings imports undo REF...
```

```bash
proton mail settings imports undo jane@fastmail.com
```

## `labels`

Labels, which a message carries.

Holds `create`, `delete`, `list`, `reorder` and `update`.

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

| Flag | Description |
| --- | --- |
| `--limit int` | How many labels per page; 0 for all of them |
| `--page int` | Which page of results, counting from zero |

### `labels reorder`

Set the order your labels are kept in.

Name the labels that should come first, in order; the rest keep the order they are in. --alphabetical sorts them all instead.

```
proton mail settings labels reorder [REF...]
```

```bash
proton mail settings labels reorder Work
proton mail settings labels reorder --alphabetical
```

| Flag | Description |
| --- | --- |
| `--alphabetical` | Sort them all alphabetically instead of naming any |

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

Holds `allow`, `block`, `list`, `remove` and `spam`.

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

### `senders list`

List every standing decision about a sender.

```
proton mail settings senders list
```

```bash
proton mail settings senders list
```

| Flag | Description |
| --- | --- |
| `--desc` | Reverse the order |
| `--limit int` | How many rules per page; 0 for all of them |
| `--page int` | Which page of results, counting from zero |
| `--sort string` | Order by: sender, since (default `sender`) |

### `senders remove`

Drop a standing decision, letting the spam filter decide again.

```
proton mail settings senders remove EMAIL...
```

```bash
proton mail settings senders remove billing@example.com
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
proton mail settings set pm-signature off
proton mail settings set view-mode conversations
proton mail settings set font-face georgia
```

## `smtp-tokens`

Tokens that let a device or a service send mail from your address.

A token sends from one custom domain address and nothing else. The address is the SMTP username and the token is the password, at smtp.protonmail.ch on port 587 with TLS.

The token is shown once, when it is made.

Holds `create`, `delete`, `get` and `list`.

### `smtp-tokens create`

Make a token for a device or a service to send with.

REF is one of your custom domain addresses, and --name is required. The token sends from that address and nothing else.

The token is shown once and never again. Under --output json it is the `token` field.

Asks for your password even when you are signed in. With no terminal to ask, pass --password-file, which takes - for stdin.

```
proton mail settings smtp-tokens create REF
```

```bash
proton mail settings smtp-tokens create billing@example.com --name 'Office printer'
proton mail settings smtp-tokens create billing@example.com --name 'Office printer' --output json
```

| Flag | Description |
| --- | --- |
| `--name string` | Name for the new token |
| `--password-file string` | Read the account password from a file, or - for stdin |
| `--totp string` | Two-factor code |

### `smtp-tokens delete`

Stop a token working, for good.

The device or the service holding it cannot send from then on. Nothing brings a token back; make another and set the device up again.

Asks for your password even when you are signed in. With no terminal to ask, pass --password-file, which takes - for stdin.

```
proton mail settings smtp-tokens delete REF...
```

```bash
proton mail settings smtp-tokens delete 'Office printer'
```

| Flag | Description |
| --- | --- |
| `--password-file string` | Read the account password from a file, or - for stdin |
| `--totp string` | Two-factor code |

### `smtp-tokens get`

Show a token and where the device it is for connects.

The token itself is not here: it was shown once, when it was made. One you have lost is deleted and made again.

```
proton mail settings smtp-tokens get REF
```

```bash
proton mail settings smtp-tokens get 'Office printer'
```

### `smtp-tokens list`

List the tokens on the account, newest first.

LAST USED is when something last sent with a token, and - where nothing ever has. The tokens themselves are never shown here.

```
proton mail settings smtp-tokens list
```

```bash
proton mail settings smtp-tokens list
```

| Flag | Description |
| --- | --- |
| `--desc` | Reverse the order |
| `--limit int` | How many smtp tokens per page; 0 for all of them |
| `--page int` | Which page of results, counting from zero |
| `--sort string` | Order by: created, name, address, last-used (default `created`) |

---

Every command also takes the [flags that work everywhere](../about/commands.md#flags-that-work-on-every-command).
