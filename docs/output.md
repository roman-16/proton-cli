# Output, JSON and exit codes

Data goes to stdout; progress bars, confirmations, footers, warnings, prompts and errors go to stderr. So a redirect gives you clean data and you still see what happened. Five response shapes, one JSON envelope per list, and exit codes that tell failures apart.

## The five shapes

### Collections

```console
$ proton mail messages list --unread --page-size 3
ID        FROM              SUBJECT                DATE              FLAGS
────────  ────────────────  ─────────────────────  ────────────────  ─────
5bH2mQxK  Fastmail Billing  Invoice #2291 ready    2026-04-15 14:32  ●★2
9xL4pQrT  Trailhead         Weekly digest          2026-04-15 09:02  ●
2mNp7RsV  Jane Roe          Re: Quarterly numbers  2026-04-14 17:48

3 of 47 messages. Next page: --page 1
```

`ID` is always first. `FLAGS` reads `●` unread, `★` starred, and a number for attachments.

An empty collection prints nothing on stdout, so a redirect yields an empty file rather than a stray header. It says `No messages.` on stderr, or `No messages match.` when a filter was applied.

Every `list` takes `--page` and `--page-size`. Everything except mail also takes `--sort` and `--desc`; each listing offers only the keys it has and says so when given another. Mail comes back newest first, ordered by Proton.

### Records

```console
$ proton drive items get /Documents/report.pdf
Name:       report.pdf
Location:   /Documents
Type:       file
MIME Type:  application/pdf
Uploaded:   2026-04-02 11:20
Signature:  verified
Size:       2.4 MB
Shared:     yes
ID:         7Kd91mQx
```

### Documents

Decrypted content meant to be read: a header block, a blank line, the body, and whatever trails it. `--body-only` gives you exactly the body.

```console
$ proton mail messages get 5bH2mQxK
Subject:    Invoice #2291 is ready
From:       Fastmail Billing <billing@fastmail.com>
Date:       2026-04-15 14:32
Signature:  verified
ID:         5bH2mQxK

Hi Roman, your invoice is attached.

Attachments
ID        NAME              SIZE
────────  ────────────────  ───────
kQ81mDx4  invoice-2291.pdf  84.2 KB
```

### Mutations

```console
$ proton mail messages trash --unread --older-than 30d
✓ Moved 12 messages to trash.
```

When something is created, its ID goes to **stdout** and the confirmation to stderr, so capturing it is a plain assignment:

```bash
LABEL=$(proton mail settings labels create --name Work)
```

### Streams

`mail messages watch` and `calendar reminders watch` stay attached and print a line the moment something happens.

```console
$ proton mail messages watch
14:32  5bH2mQxK  Fastmail Billing      Invoice #2291 ready
14:41  9xL4pQrT  Trailhead             Weekly digest
```

In JSON, each line is one object - what `jq` reads without `--slurp`. There is no envelope and no footer ([why](design-notes.md#why-a-stream-has-no-footer)). Ctrl+C or SIGTERM ends a watch quietly with exit `0`, so a service manager doesn't log a failure every time you stop it.

## Errors

One line for the problem, an indented `Try:` block for the fix.

```console
$ proton mail messages get 5bH2mQxK --render htm
Error: --render accepts: text, html, raw.
```

Anything judgeable from your command line alone - a misspelled setting key, an impossible `--render`, a colour outside Proton's palette - is caught before signing in or contacting Proton.

## Exit codes

| Code | Meaning |
| --- | --- |
| `0` | Success |
| `1` | Something you passed was wrong |
| `2` | Authentication failed |
| `3` | Not found |
| `4` | Ambiguous, or a conflict |
| `5` | Network or server problem |
| `6` | Refused by your [confirmation policy](language.md#deny) |
| `130` | Cancelled with `Ctrl+C` |

## JSON and YAML

```bash
proton mail messages list --output json
proton mail messages list --output yaml
```

**Every collection is one envelope**, keyed by its plural name:

```json
{
  "messages": [ { "id": "5bH2…", "subject": "Invoice #2291 is ready", "unread": true } ],
  "count": 3,
  "total": 47,
  "page": 0,
  "page_size": 3,
  "has_more": true
}
```

`count` is always there. `total`, `page`, `page_size` and `has_more` appear when the request involved them, so a consumer can tell "page 0" from "not paginated".

**A list is always a list.** An empty collection is `[]` and an empty map is `{}`, never a missing key - so `jq '.attendees[]'` iterates nothing rather than failing on `null`. Scalars are the other way round: a field that does not apply is absent. That is how `occurrence_count` answers only when there is a number to give - an event that does not recur has none, and neither does a series with no end, which `rrule` tells you apart.

**Names, not numbers**, matching the text output and `set`: `{ "type": "file", "state": "active", "unread": true }`. Keys are `snake_case`, timestamps are `<verb>_time` in Unix seconds, sizes are `size` in bytes, and IDs are always complete.

**Times come back in the zone you are working in** - `--zone`, or `TZ`, or `zone:` in your [config](configuration.md), or your system's. An event's `start` and `end` are RFC 3339 with that offset, and what the event itself is anchored to is its own field:

```json
{ "start": "2026-04-16T16:00:00+02:00", "end": "2026-04-16T17:00:00+02:00", "zone": "Europe/Vienna" }
```

An all-day event has `"all_day": true`, begins at midnight on the date it names, and ends at the midnight after its last day - so `end - start` is how long it lasts.

Mutations emit JSON too:

```console
$ proton mail settings labels create --name Work --output json
{ "action": "created", "count": 1, "dry_run": false, "ids": ["kQ81mDx4T9…"], "kind": "label", "name": "Work" }
```

The one exception is [`proton api`](apps/api.md), which passes Proton's response through unchanged.

## Colour, width and quiet

Colour marks the parts that carry a verdict: `✓` green, `!` yellow, `Error:` red, IDs magenta, `●` unread, `★` starred, and the signature line green, yellow or red. A `■` beside a label, folder, calendar or group is the exact colour Proton stores for it. Everything else stays plain.

Shades come from your terminal's theme, not from proton ([why](design-notes.md#why-colour-is-asked-for-by-name)). Colour is off whenever output is piped or redirected, under `--output json` or `yaml`, and with `--no-color` or `NO_COLOR`. It never changes the layout and never carries meaning on its own - every verdict is spelled out.

On a terminal, a table too wide to fit gives up room from its widest flexible column, never from a date or an ID. Piped output is never truncated, and widths are measured in terminal cells, so a subject in Japanese or a filename with an emoji stays aligned.

`--quiet` silences confirmations, notes, footers and progress. It never silences the answer.

## Prompts

Only `account login` and the commands that need your password again ever ask a question, and only when standard input is a terminal. Everything else fails with a message rather than waiting, so a scheduled job never hangs. Prompts go to stderr.

```bash
proton account login --no-input     # fail instead of asking
PROTON_NO_INPUT=1 proton account login
```
