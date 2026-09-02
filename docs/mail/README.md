# Mail

Read, send, search and organize Proton Mail from your terminal. Bodies are decrypted on your machine, and outgoing mail is encrypted and signed with your address key, exactly like the web client.

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

`get` shows the plain-text body by default. `--render html` gives you the original markup, `--render raw` the untouched body, and `--strip-quotes` drops quoted reply blocks.

A `Signature:` line reports the verdict of the signature check against the sender's key.

## Search

Searching is `list` with a filter, because it is one request to Proton either way.

`list` opens on the inbox, so **`--folder all` is what widens it to everything.**

```bash
proton mail messages list --keyword invoice --folder all
proton mail messages list --from billing@example.com --after 2026-01-01 --folder all
proton mail messages list --subject "Q1 report" --folder archive
```

`--from` and `--to` match addresses. `--keyword` also matches display names and body text.

Proton's index lags a change by a few seconds ([why](../about/why.md#why-search-lags)).

## Send

```bash
proton mail messages send --to alice@proton.me --subject Hi --body "Hello there"
proton mail messages send --to alice@proton.me --subject Report --body "See attached." --attach ./report.pdf
proton mail messages send --to alice@proton.me --subject Hi --body "<b>Hi</b>" --html
echo "Deployed." | proton mail messages send --to me@proton.me --subject Deploy --body -
```

`send` prints the new message ID on stdout, so `ID=$(proton mail messages send …)` works.

**Your signature is applied automatically**, as in the web client: the address's own signature, plus Proton's *"Sent with Proton Mail secure email."* footer when your account has it enabled. Free accounts have that footer forced on.

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
pass show mail/jane | proton mail messages send … --eo-password-stdin
```

The password a recipient outside Proton types is a secret, so it comes from a file or from standard input rather than from a flag value. Proton asks for at least eight characters.

Such a message expires after 28 days whatever `--expires` says. `--eo-password-stdin` takes the stream for itself, so it cannot be combined with `--body -`.

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

Add `--dry-run` to see the list first. `--limit` defaults to 150, which is Proton's per-page cap. See [Filters and bulk changes](../using/filters.md).

**`empty` is not `delete --all`.** A filtered delete enumerates what it will touch and shows you. `empty` asks Proton to clear the folder without ever naming its contents, which is why it takes no filter and always asks.

```bash
proton mail messages empty --folder trash
proton mail messages expire REF --in 7d        # delete itself later; --never stops it
proton mail messages unsubscribe REF           # ask a mailing list to stop
```

`unsubscribe` uses whatever the message offered: a `List-Unsubscribe` header, or the one-click form behind it. Proton sends the request, because Proton is the party the list already knows.

## Threads

Conversations are whole threads, with the same verbs as messages.

```bash
proton mail conversations get REF              # every message, chronological
proton mail conversations get --summary REF    # one line per message
proton mail conversations snooze REF --until 3d
```

Snooze works on **threads**, not messages, because that is what Proton snoozes: a conversation leaves the inbox as a whole and returns as a whole.

`--until` takes a duration from now or a moment. A moment in the past is refused.

## Attachments

```bash
proton mail messages attachments list REF
proton mail messages attachments download REF --dest-dir ./attachments/
proton mail conversations attachments download REF --dest-dir ./thread/
```

Existing files are never overwritten silently: names collide into `file (2).pdf`, or pass `--force`. `--include-inline` covers embedded images too.

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

**Exported files are not encrypted.** Their `DKIM-Signature` and `ARC-*` headers no longer verify either, since the body those headers signed was the encrypted one. The web client's export behaves the same way.

Going the other way, `--eml` reads a file back into a draft or a send:

```bash
proton mail drafts create --eml ./message.eml
proton mail messages send --eml ./message.eml --to someone-else@proton.me
```

Any flag you also pass overrides what the file says, and no signature is appended: the file is already a finished message.

There is no way to place an old message into your archive. Proton exposes no endpoint that ingests one, for any client.

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
proton mail settings folders create --name Clients --parent PARENT_FOLDER_ID
proton mail settings labels create --name Important --color "#8080FF"
proton mail settings labels delete Important        # by name, or by ID
```

Colours have to be one of Proton's 20 accent colours. An invalid one prints the palette and is refused before anything is sent.

Deleting a folder or label asks first. The messages it held are not deleted.

Folders carry a **NOTIFY** switch, which is whether mail landing there is worth telling you about. It shows in `folders list`, `--notify` sets it, and it decides what `messages watch` covers by default. Proton offers it on folders alone, so labels have neither.

## Filters

Server-side filters, the same ones the web client creates. Describe one with `--if` and Proton writes the [Sieve](https://en.wikipedia.org/wiki/Sieve_(mail_filtering_language)) - the same script the web client's builder produces, so a filter made here opens in it.

```bash
proton mail settings filters create --name "Archive invoices" \
  --if "subject contains invoice" --move-to Archive

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

Actions are `--move-to` (one folder), `--label` (repeatable), `--mark-read` and `--star`. A filter needs at least one. Every filter skips mail Proton has already called spam.

`update` rewrites the rule in place and replaces the whole thing rather than adding to it. The filter keeps its place in the order and stays enabled or disabled as it was, which is what makes this different from deleting it and making a new one.

`get` shows the rule as it stands, in the words `--if` takes. To bring your own script, use `--sieve -`; `get` then shows the script itself.

A filter acts once, as mail arrives, so a rule written today does nothing about yesterday's mail:

```bash
proton mail settings filters apply                       # every enabled filter
proton mail settings filters reorder Newsletters Receipts Archive
```

Order decides the outcome, because the first rule to file a message wins. `reorder` replaces the **whole** order and refuses a partial one.

## Auto-reply

```bash
proton mail settings autoreply set --repeat fixed \
  --start 2026-07-01T09:00 --end 2026-07-14T18:00 \
  --message "I'm away until the 14th."
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

Proton sends every auto-reply with the subject `Auto` and offers no way to change it. Auto-reply is a paid feature.

## Who reaches the inbox

Proton's settings page shows three lists - spam, block, allow - but they are one record with a destination on it, so `list` shows all three:

```bash
proton mail settings senders list
proton mail settings senders block spammer@example.com
proton mail settings senders block @example.com          # a whole domain
proton mail settings senders allow billing@example.com
proton mail settings senders forget billing@example.com
```

A decision applies before the spam filter forms an opinion. Deciding again about the same sender replaces the earlier decision. `forget` lets the filter decide again.

## Settings

```bash
proton mail settings              # everything, at a glance
proton mail settings set          # the writable keys, grouped by page
proton mail settings set view-mode conversations
proton mail settings addresses update me@proton.me --display-name "Roman L."
proton mail settings addresses update me@proton.me --signature - < signature.html --html
```

Every key has a fixed set of values, checked before anything is sent. Values can be given by name or by Proton's own number.

Signatures are stored as HTML. Plain text is escaped and its newlines become line breaks; `--html` passes markup through.
