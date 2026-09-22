# Mail

Read, send, search and organize Proton Mail from your terminal. Bodies are decrypted on your machine, and outgoing mail is encrypted and signed with your address key.

This page is what people actually do. For every command and flag, see the reference: [messages](messages.md), [conversations](conversations.md), [drafts](drafts.md), [settings](settings.md).

## Read your mail

```bash
proton mail messages list                    # the inbox
proton mail messages list --unread
proton mail messages list --folder archive
proton mail messages get "Invoice #2291"     # headers, body, attachment list
proton mail messages get REF --body-only > body.txt
```

Folders are `inbox`, `sent`, `drafts`, `trash`, `spam`, `archive`, `starred`, `scheduled`, `snoozed`, `all`, or any label. Proton's inbox tabs are folders too: `social`, `promotions`, `updates`, `newsletters`, `transactions`.

Listings come back newest first. `--sort size` orders by how much room a message takes, largest first, and `--desc` reverses either order. A listing ordered by size shows a SIZE column and is answered by Proton, so it does not search bodies.

```bash
proton mail messages list --folder all --sort size --limit 10
```

`get` shows the plain-text body by default. `--render html` gives you the original markup, `--render raw` the untouched body, and `--strip-quotes` drops quoted reply blocks.

A `Signature:` line reports the verdict of the signature check against the sender's key.

## Spot a message Proton distrusts

A message Proton flagged carries two more lines:

```console
$ proton mail messages get 5bH2mQxK
Subject:    Your account will be suspended
From:       PayPal <security@paypa1-secure.com>
To:         me@proton.me
Date:       2026-04-15 14:32
Signature:  unsigned
DMARC:      failed
Flagged:    phishing
ID:         5bH2mQxK
```

`Flagged:` reads `phishing`, `suspicious`, or both. `DMARC: failed` means the sender's domain did not vouch for the message, so the address it claims to come from may not be the address it came from.

`list` marks the same messages with `✗` in the FLAGS column. In `--output json` each verdict is a field of its own: `phishing`, `suspicious` and `dmarc_failed`, present only when true.

```bash
proton mail messages mark legitimate REF      # Proton was wrong about this one
proton mail messages mark phishing REF        # report it, and file it as spam
```

`mark legitimate` settles the `Flagged:` verdict on that message alone, and the `DMARC:` line stays. To let a sender through from now on, see [Who reaches the inbox](#who-reaches-the-inbox).

`mark phishing` sends the message to Proton decrypted, body included, for their anti-abuse team to read. Nothing withdraws a report. To keep a sender out without reporting anything, use `proton mail settings senders block`.

## Search

Searching is `list` with a filter. `list` looks in the inbox; **`--folder all` searches everything.**

```bash
proton mail messages list --keyword invoice --folder all
proton mail messages list --from billing@example.com --after 2026-01-01 --folder all
proton mail messages list --subject "Q1 report" --folder archive
```

`--from` and `--to` match addresses. `--keyword` matches the subject, a display name and an address.

Bodies are not searched. To search what a message says, build an index on this machine: [Local index](../index/README.md). With one, `--keyword` covers bodies and `--from` covers display names.

`--after` and `--before` are the first and last whole days to include, read in your own zone, and both are included.

A change reaches `list` a few seconds late. To confirm one right away, `get` the ID the command printed.

## Send

```bash
proton mail messages send --to alice@proton.me --subject Hi --body "Hello there"
proton mail messages send --to alice@proton.me --subject Report --body "See attached." --attach ./report.pdf
proton mail messages send --to alice@proton.me --subject Hi --body "<b>Hi</b>" --html
echo "Deployed." | proton mail messages send --to me@proton.me --subject Deploy --body -
```

`send` prints the new message ID on stdout, so `ID=$(proton mail messages send …)` works.

**Your signature is applied automatically**: the address's own signature, plus Proton's *"Sent with Proton Mail secure email."* footer when your account has it enabled. Free accounts have that footer forced on.

- `--no-signature` leaves both out of one message.
- `proton mail settings set pm-signature off` turns the footer off account-wide.

On an account with several addresses, `--from` chooses which one it leaves from, plus-aliases included:

```bash
proton mail messages send --from work@example.com --to alice@proton.me --subject Hi --body Hello
```

### Scheduling, expiry, and outside recipients

```bash
proton mail messages send … --send-at 2026-05-01T09:00     # local time; confirms the resolved time
proton mail messages send … --expires 7d
proton mail messages send … --eo-password-file /run/secrets/jane --eo-password-hint "our usual"
pass show mail/jane | proton mail messages send … --eo-password-file -
```

The password a recipient outside Proton types comes from a file or from standard input, never from a flag value. It needs at least eight characters.

Such a message expires after 28 days whatever `--expires` says. `--eo-password-file -` takes the stream for itself, so it cannot be combined with `--body -`.

To read one that was sent to you, see [Open a password-protected message somebody sent you](#open-a-password-protected-message-somebody-sent-you).

A scheduled send sits in the `scheduled` folder until it goes. To pull it back to Drafts, run `proton mail messages unschedule REF`.

## Reply and forward

```bash
proton mail messages reply "Invoice #2291" --body "Thanks, paid today."
proton mail messages reply REF --everyone --body "Looping in the team."
proton mail messages forward REF --to alice@proton.me --body "FYI"
proton mail conversations reply REF --body "Works for me."   # newest message in a thread
```

The original is quoted below your text, the subject gains `Re:` or `Fw:` (never twice), and the thread stays a thread.

A reply leaves from the address the original arrived on. A forward carries the original's attachments without re-uploading them.

- `--no-quote` sends your text alone.
- `--no-attachments` leaves them behind.
- `--draft` stops before sending and prints the new draft's ID, the same as clicking Reply and leaving the composer open.

Reply and forward take everything `send` takes.

## Read receipts

Ask the people you write to for confirmation that they read the message:

```bash
proton mail messages send --to alice@proton.me --subject Contract --body "Signed and attached." --request-receipt
```

Every command that composes takes the flag: `send`, `reply`, `forward`, both `conversations` answers, and `drafts create`. On a draft, `proton mail drafts update REF --request-receipt=false` takes the request off again.

A message that asks you for one says so:

```console
$ proton mail messages get 5bH2mQxK
Subject:    Invoice #2291 is ready
From:       Fastmail Billing <billing@fastmail.com>
To:         me@proton.me
Date:       2026-04-15 14:32
Receipt:    requested
Signature:  verified
ID:         5bH2mQxK
```

Nothing goes back until you send it:

```bash
proton mail messages receipt 5bH2mQxK
```

The line then reads `Receipt: sent`. A receipt cannot be taken back, and a message that already has one is refused.

In `--output json` a message carries `receipt_requested`, `receipt_sent`, and `receipt_due` for the ones you can still answer.

## Drafts

A draft is a message, so `messages get`, `move` and the rest already work on one. `mail drafts` holds what only makes sense before it goes out.

```bash
proton mail drafts create --to alice@proton.me --subject Report --body "Draft one."
proton mail drafts update REF --body "Draft two." --attach ./report.pdf --detach old-annex.xlsx
proton mail drafts send REF
```

`update` replaces only what you pass. `--to`, `--cc` and `--bcc` replace the whole list; `--attach` adds a file and `--detach` removes one.

`REF` resolves within Drafts only, so editing "Report" can never reach a message you already sent. Sending a draft delivers it exactly as stored.

## Organize

A message lives in exactly **one folder** and carries any number of **labels**. So moving and labelling are different verbs, and passing a label to `move` is an error rather than a silent relabel.

```bash
proton mail messages move REF --into archive     # it leaves where it was
proton mail messages label REF --label Work      # it stays where it is
proton mail messages mark read REF
proton mail messages star REF
proton mail messages trash REF
```

Every one of those takes filters instead of references, and acts on everything that matches:

```bash
proton mail messages trash --unread --older-than 30d
proton mail messages move --into archive --from newsletter@example.com --older-than 7d
proton mail messages delete --folder spam --all
```

Add `--dry-run` to see the list first. `--limit` caps how many a verb touches and defaults to 150. See [Filters and bulk changes](../using/filters.md).

**`empty` is not `delete --all`.** A filtered delete shows what it will touch. `empty` clears the folder whole, takes no filter, and always asks.

```bash
proton mail messages empty --folder trash
proton mail messages update REF --expires 7d   # delete itself later
proton mail messages update REF --expires never
proton mail messages unsubscribe REF           # leave the list a message came from
```

`unsubscribe` uses whichever way the list offers, and the answer says which it used. To work from the senders instead of their mail, see [Mailing lists](#mailing-lists).

## Mailing lists

`mail mailing-lists` is the mailbox read by sender: everyone who writes to you as a list, how much they send, and how much of it you never open.

```bash
proton mail mailing-lists list
proton mail mailing-lists list --sort unread --desc
proton mail mailing-lists list --unsubscribed        # the ones you have left
proton mail mailing-lists get "Trailhead Weekly"
```

`REF` is the list's name, its sender address, or its ID. Order with `--sort name|unread|frequency|received|read`, newest first, and `--desc` reverses.

```bash
proton mail mailing-lists unsubscribe "Trailhead Weekly"
proton mail mailing-lists update "Trailhead Weekly" --into Archive --mark-read
proton mail mailing-lists remove "Trailhead Weekly"
```

`unsubscribe` uses one of three ways, and says which: Proton submits a one-click form for you, an unsubscribe address gets a message sent from the address the list writes to, or a link opens in your browser and is printed either way. A list that offers none of them is refused, and the answer points at `proton mail settings senders block`.

`update` covers the mail already here and everything that arrives afterwards. It leaves a filter behind, which `get` names; turn the rule off with `proton mail settings filters disable`.

`remove` drops the entry from the listing. It unsubscribes from nothing, and the list comes back the next time it writes.

## Threads

Conversations are whole threads, with the same verbs as messages.

```bash
proton mail conversations get REF              # every message, chronological
proton mail conversations get --summary REF    # one line per message
proton mail conversations snooze REF --until 3d
```

Snooze works on **threads**, not messages: a conversation leaves the inbox as a whole and returns as a whole.

`--until` takes a duration from now or a moment. A moment in the past is refused.

## Attachments

```bash
proton mail messages attachments list REF
proton mail messages attachments download REF --dest-dir ./attachments/
proton mail messages attachments download REF invoice-2291.pdf --dest-dir .
proton mail conversations attachments download REF --dest-dir ./thread/
```

Naming an attachment downloads that one, by its own name or by its ID; naming none downloads them all.

Existing files are never overwritten silently: names collide into `file (2).pdf`, or pass `--force`. `--include-inline` covers embedded images too.

## Open a password-protected message somebody sent you

A message sent to an address outside Proton arrives as a link, and the password comes to you some other way. Name it by the link or by the id in it:

```bash
proton mail protected get 9fK2pQ7xNv4mB8 --eo-password-file /run/secrets/jane
proton mail protected get 'https://mail.proton.me/eo/9fK2pQ7xNv4mB8' --eo-password-file -
proton mail protected attachments list 9fK2pQ7xNv4mB8 --eo-password-file /run/secrets/jane
proton mail protected attachments download 9fK2pQ7xNv4mB8 --eo-password-file /run/secrets/jane --dest-dir .
```

Quote the link: a shell splits nothing in an id, and everything in a URL.

The password is the one whoever sent the message gave you, not a Proton password, and it comes from a file or from standard input.

```console
$ proton mail protected get 9fK2pQ7xNv4mB8 --eo-password-file /run/secrets/jane
Subject:  Q3 numbers
From:     Jane Roe <jane@proton.me>
To:       me@example.com
Date:     2026-04-15 14:32
Expires:  2026-05-13 14:32

Here are the numbers we discussed.

Attachments
ID        NAME           SIZE
────────  ─────────────  ───────
kQ81mDx4  q3-report.pdf  84.2 KB
```

`get` takes the same `--render`, `--body-only` and `--strip-quotes` as `mail messages get`. The message is gone 28 days after it was sent.

An answer goes to whoever sent it and nobody else:

```bash
proton mail protected reply 9fK2pQ7xNv4mB8 --eo-password-file /run/secrets/jane --body 'Got them, thanks.'
proton mail protected reply 9fK2pQ7xNv4mB8 --eo-password-file /run/secrets/jane --body 'Signed copy attached.' --attach ./signed.pdf
```

Five answers can go back from behind one link, and a `Replies` line on `get` counts the ones already sent.

**None of this needs an account.** Your own mail, `login` included, is untouched by it.

## Back it up

`export` writes ordinary RFC 822 files you can open in any mail client.

```bash
proton mail messages export --folder archive --older-than 1y --all --dest-dir ./backup
proton mail messages export --folder inbox --all --format mbox --dest inbox.mbox
proton mail messages export REF --dest - | formail -X ""
```

It takes the same filters as `trash` and `move`.

- `--format eml` writes one file per message, named `<date> <subject>.eml`.
- `--format mbox` concatenates everything into one file.
- `--no-attachments` skips downloads, which is much faster for a large archive.

**Exported files are not encrypted.** Their `DKIM-Signature` and `ARC-*` headers no longer verify either.

Going the other way, `--eml` reads a file back into a draft or a send:

```bash
proton mail drafts create --eml ./message.eml
proton mail messages send --eml ./message.eml --to someone-else@proton.me
```

Any flag you also pass overrides what the file says, and no signature is appended: the file is already a finished message.

A file cannot be placed straight into your archive. Mail comes in from another mailbox instead - see [Bring mail over from another account](#bring-mail-over-from-another-account).

## Watch for new mail

```bash
proton mail messages watch
proton mail messages watch --output json | jq -c .
```

Stays attached and prints a line the moment a message lands.

It reports what happens while it is watching, so nothing that arrived beforehand comes up. A thread coming back from snooze counts as landing.

Without `--folder` it covers the inbox plus every folder whose notifications are on.

Each JSON line names the thread in `conversation_id`, so a consumer can act on the whole thread without looking it up. See [Streams](../using/output.md#streams) and [Desktop notifications](../using/scripting.md#desktop-notifications).

## Folders and labels

```bash
proton mail settings folders create --name Projects
proton mail settings folders create --name Clients --parent Projects
proton mail settings labels create --name Important --color "#8080FF"
proton mail settings labels delete Important        # by name, or by ID
```

Colours have to be one of Proton's 20 accent colours. An invalid one prints the palette and is refused before anything is sent.

A folder goes inside another with `--parent`, which takes the folder's name, short ID or ID. `--parent none` brings it back to the top level.

Deleting a folder or label asks first. The messages it held are not deleted.

### Order

`folders list` and `labels list` show them in the order Proton keeps them, which is the order every Proton client shows.

```bash
proton mail settings folders reorder Projects        # put it first
proton mail settings labels reorder --alphabetical   # sort them all
```

`reorder` moves the ones you name to the front, in the order you name them, and the rest keep the order they are in. A folder is ordered among the folders it sits beside, so name folders from one place at a time; `--alphabetical` covers every level.

Folders carry a **NOTIFY** switch, which is whether mail landing there is worth telling you about. It shows in `folders list`, `--notify` sets it, and it decides what `messages watch` covers by default. Labels have no such switch.

## Filters

Server-side filters. Describe one with `--if`; it is stored as [Sieve](https://en.wikipedia.org/wiki/Sieve_(mail_filtering_language)) and opens in the web client's filter builder afterwards.

```bash
proton mail settings filters create --name "Archive invoices" \
  --if "subject contains invoice" --into Archive

proton mail settings filters create --name Receipts \
  --if "sender contains billing@" --if "subject not contains draft" \
  --label Receipts --mark-read

proton mail settings filters create --name Loud --match any \
  --if "subject starts [ALERT]" --if "attachments contains" --star
```

A condition reads `FIELD [not] COMPARATOR VALUE`:

| Part | Values |
| --- | --- |
| Field | `subject`, `sender`, `recipient`, `attachments` |
| Comparator | `contains`, `is`, `starts`, `ends`, `matches` |

`is` wants the whole value. `matches` takes `*` and `?` as wildcards. `not` inverts. `attachments` takes no value: it asks whether there is one.

Every condition has to hold unless you pass `--match any`.

Actions are `--into` (one folder), `--label` (repeatable), `--mark-read` and `--star`. A filter needs at least one. Every filter skips mail Proton has already called spam.

`update` rewrites the rule in place and replaces the whole thing rather than adding to it. The filter keeps its place in the order and stays enabled or disabled as it was, which is what makes this different from deleting it and making a new one.

`get` shows the rule as it stands, in the words `--if` takes. To bring your own script, use `--sieve -`; `get` then shows the script itself.

A filter acts once, as mail arrives, so a rule written today does nothing about yesterday's mail:

```bash
proton mail settings filters apply                       # every enabled filter
proton mail settings filters reorder Receipts            # run this one first
```

Order decides the outcome: the first rule to file a message wins. `reorder` moves the filters you name to the front and leaves the rest in the order they are in.

## Auto-reply

```bash
proton mail settings autoreply set --repeat fixed \
  --start 2026-07-01T09:00 --end 2026-07-14T18:00 \
  --body "I'm away until the 14th."
proton mail settings autoreply disable
```

`--start` and `--end` are written in the grammar the repeat mode dictates:

| `--repeat` | `--start` / `--end` |
| --- | --- |
| `fixed` | `2026-07-01T09:00` - a date and time |
| `daily` | `09:00` - a time of day, with `--days mon,tue,wed` |
| `weekly` | `mon:09:00` - a weekday and time |
| `monthly` | `1:09:00` - a day of the month and time |
| `permanent` | *not used* |

All of them take `--zone`, any IANA name, defaulting to your system's.

Saving a schedule turns the auto-reply on. `disable` keeps it for later.

Every auto-reply goes out with the subject `Auto`, which cannot be changed. Auto-reply is a paid feature.

## Who reaches the inbox

Spam, block and allow are one list with a destination on each entry, so `list` shows all three:

```bash
proton mail settings senders list
proton mail settings senders block spammer@example.com
proton mail settings senders block @example.com          # a whole domain
proton mail settings senders allow billing@example.com
proton mail settings senders remove billing@example.com
```

A decision applies before the spam filter forms an opinion. Deciding again about the same sender replaces the earlier decision. `remove` lets the filter decide again.

## Forward one of your addresses

```bash
proton mail settings forwarding create me@proton.me jane@proton.me
proton mail settings forwarding create me@proton.me jane@example.com
proton mail settings forwarding list
```

Setting one up needs a paid Mail plan. Nothing is forwarded until the other side accepts. A Proton address accepts in its Proton client or with `forwarding accept`; an address outside Proton follows the link Proton emails it.

Forwarding to an address outside Proton turns off end-to-end encryption for your address. It comes back on when the last such forwarding from that address is deleted. `proton mail settings addresses get me@proton.me` shows whether it is on.

**Setting up that kind asks for your password**, and so does the `delete` that turns encryption back on. With no terminal, pass `--password-file`, which takes `-` for standard input. The other commands that do this are listed in [Account](../account/README.md#commands-that-ask-for-the-password-again).

## Change or stop a forwarding you set up

```bash
proton mail settings forwarding disable jane@proton.me
proton mail settings forwarding enable jane@proton.me
proton mail settings forwarding resend jane@proton.me
proton mail settings forwarding delete jane@proton.me
```

`disable` stops mail being forwarded and keeps the arrangement. `resend` offers it again to a forwardee who declined it, or whose material your key change left outdated. `delete` takes one down in either direction.

## Accept a forwarding somebody sent you

```bash
proton mail settings forwarding list
proton mail settings forwarding accept jane@proton.me
proton mail settings forwarding decline jane@proton.me
```

A forwarding is named by the other party's address, in either direction. Only a pending forwarding sent to one of your addresses can be accepted or declined, and accepting needs no plan.

## Set up a custom domain

```bash
proton mail settings domains create example.com
proton mail settings domains get example.com
proton mail settings domains list
```

Adding a domain needs a paid Mail plan, and how many you may have depends on it. Without one, every command here says so.

**Changing a domain asks for your password**, even when you are signed in. That is `create`, `update` and `delete`. With no terminal, pass `--password-file`, which takes `-` for standard input. The other commands that do this are listed in [Account](../account/README.md#commands-that-ask-for-the-password-again).

`get` shows every DNS entry the domain needs and re-checks your DNS each time it runs. Add the entries at your registrar, then run it again. Each check reads `ok`, or what Proton found instead: `missing`, `wrong`, `duplicate`, `wrong-priority`, `backup`, `error`, `warning`, `delegated` or `relaxed`.

```console
$ proton mail settings domains get example.com
Domain:        example.com
Status:        unverified
Addresses:     0
Catch-all:     (none)
Verification:  missing
               TXT  @  protonmail-verification=7c1e4f9a2b8d6503e1f74a0cc9b2d81e5f3a6740
```

No address can be added on the domain until Proton has seen the verification entry. Every entry has to stay in place afterwards: removing the verification entry gives the domain up.

`list` shows the last check rather than making a new one. Its `DNS` column reads `ok`, or names the checks that are not passing.

## Catch mail sent to a name that does not exist

```bash
proton mail settings domains update example.com --catch-all work@example.com
proton mail settings domains update example.com --catch-all none
```

Mail sent to a name your domain has not got arrives at the catch-all address instead of being refused. `--catch-all` takes one of your addresses on that domain, or `none` to refuse such mail again. One address catches at a time, so naming another moves it.

## Remove a custom domain

```bash
proton mail settings domains delete example.com
```

Every address on the domain stops sending and receiving, and the mail they hold stays. Adding the domain again needs its DNS entries verified from scratch.

## Add an address

```bash
proton mail settings addresses create work@example.com --display-name Work
proton mail settings addresses list
proton mail settings addresses disable work@example.com
proton mail settings addresses enable work@example.com
proton mail settings addresses delete work@example.com
```

The address sends and receives as soon as it exists. Its domain has to be one the account can use: a Proton domain, or a custom domain already set up. Adding, disabling and deleting an address needs a paid Mail plan.

Your first Proton address and your short-domain address stay enabled and cannot be deleted. Proton allows one address deletion a year unless the address is on a custom domain, and a deleted address cannot be used again by anyone.

`get` shows whether an address holds a key. One that does not can neither send nor receive; running `create` on it publishes the missing key.

## Turn on your @pm.me address

```bash
proton mail settings addresses create alice@pm.me
```

Your short-domain address is your username at `pm.me`: the local part of the address `addresses list` shows as `original`. It needs a paid Mail plan, takes the display name and signature of your default address, and once on cannot be disabled or deleted. Any other address on `pm.me` counts towards your address limit.

## Make another address the default

```bash
proton mail settings addresses reorder alice@pm.me
proton mail settings addresses reorder alice@pm.me work@example.com alice@proton.me
```

The first address is the default: `addresses list` shows it first, and mail leaves from it when no `--from` is given. Name the addresses that should come first, in order; the rest keep the order they are in. A disabled or external address, or one that cannot send or receive, cannot be the default.

## Bring mail over from another account

```bash
proton mail settings imports create jane@fastmail.com --imap-password-file /run/secrets/fastmail
proton mail settings imports list
proton mail settings imports get jane@fastmail.com
```

`create` starts an import of another mailbox over IMAP and returns. The import carries on for as long as it takes, and `list` and `get` say how far it has got.

**The other mailbox's password is sent to Proton**, which connects to the mailbox and keeps the password until the import is over. Most providers want an app password here rather than the one you sign in with. The password is read from a file, never a flag - `-` reads standard input.

`--server` and `--port` are looked up from the address. Give them for a provider Proton does not know:

```bash
proton mail settings imports create jane@example.com --imap-password-file /run/secrets/mailbox \
  --server imap.example.com --port 993
```

Every folder comes over. `--skip` leaves one out, and everything inside it, by name or by glob. `--after` and `--before` bound what is fetched by the day it arrived, and `--to` picks which of your addresses the mail belongs to:

```bash
proton mail settings imports create jane@fastmail.com --imap-password-file /run/secrets/fastmail \
  --skip Spam --skip 'Archive/*' --after 2024-01-01 --to jane@proton.me
```

A folder that matches one of yours lands in it, the rest keep their own names, and Gmail's folders arrive as labels. Everything that arrives carries one label, named after the other mailbox unless `--label` says otherwise.

```console
$ proton mail settings imports get jane@fastmail.com
Account:   jane@fastmail.com
Via:       imap
Server:    imap.fastmail.com:993
State:     importing
Messages:  2841/9302
Started:   2026-04-15 09:12
Folders:   INBOX → Inbox 1204
           Archive → Archive 1637 of 6902
           Sent → Sent 0 of 1196
ID:        b3Kd91mQ
```

`cancel` stops an import that is running and keeps what already arrived. `resume` sets a stopped one going again, with `--imap-password-file` where the other mailbox refused the connection. A delayed import is waiting on the other provider and picks itself up.

**`undo` removes everything an import brought in** - the messages, and the folders and labels made for them. Nothing brings them back; fetching the mailbox again is a new import. `delete` forgets the record and leaves the mail where it is.

Imports started in Proton's own settings are listed here too, including from a Google or Microsoft account. Starting one of those needs a browser.

## Send from a printer or another service

```bash
proton mail settings smtp-tokens create billing@example.com --name 'Office printer'
proton mail settings smtp-tokens list
proton mail settings smtp-tokens get 'Office printer'
proton mail settings smtp-tokens delete 'Office printer'
```

A token lets a device or a service send mail as one of your addresses. It sends from one custom domain address and nothing else, and needs a paid Mail plan. An address on a Proton domain cannot hold one.

**Making and deleting a token asks for your password**, even when you are signed in. With no terminal, pass `--password-file`, which takes `-` for standard input. The other commands that do this are listed in [Account](../account/README.md#commands-that-ask-for-the-password-again).

**The token is shown once**, when it is made. Set the device up with what `create` answers: the address as the SMTP username, the token as the password, and TLS turned on.

```console
$ proton mail settings smtp-tokens create billing@example.com --name 'Office printer'
Token:    9Kd2mQxT9wLpN4vRs8kZc
Address:  billing@example.com
Server:   smtp.protonmail.ch
Port:     587
Name:     Office printer
```

Under `--output json` the token is the `token` field:

```bash
TOKEN=$(proton mail settings smtp-tokens create billing@example.com --name 'Office printer' --output json | jq -r .token)
```

`get` shows everything but the token, so one you have lost is deleted and made again. `LAST USED` in the listing is when something last sent with a token, and `-` where nothing ever has. Deleting a token stops the device sending.

## Settings

```bash
proton mail settings get          # everything, at a glance
proton mail settings list         # the writable keys, grouped by page
proton mail settings set view-mode conversations
proton mail settings addresses update me@proton.me --display-name "Roman L."
proton mail settings addresses update me@proton.me --signature - < signature.html --html
```

Every key has a fixed set of values, checked before anything is sent. Values can be given by name or by number.

Signatures are stored as HTML. Plain text is escaped and its newlines become line breaks; `--html` passes markup through.
