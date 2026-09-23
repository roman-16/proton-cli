# Local index

Search inside your messages, find a file without waiting for a walk, and find an event by what it says. Proton searches subjects, names and addresses; an index on this machine searches what your account holds.

This page is what people actually do. For every command and flag, see the reference: [index](index.md).

Mail, Drive and Calendar can each be indexed. Building one downloads what it indexes and keeps an encrypted copy under `~/.config/proton-cli/index/`. Nothing in your account changes, and you can remove it at any time.

## Search inside messages

Nothing has an index until you build one. Naming no app builds all three:

```bash
proton index create mail
```

```console
$ proton index create mail
Indexing mail         ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━  100%  48213 / 48213 messages  in 3m
Indexing mail bodies  ━━━━━━━───────────────────────   26%  12400 / 48213 bodies  8.2/s  1h12m left
```

Mail is indexed in two passes. The first takes minutes and holds every message; from then on a filtered listing such as `--unread`, `--from` or `--after` is answered from this machine. The second downloads the bodies newest first, and for a large mailbox takes hours.

Stopping it with Ctrl+C is safe: running it again carries on where it stopped.

Once a body is indexed `--keyword` searches it, everywhere the flag is taken:

```bash
proton mail messages list --keyword "parking permit" --folder all
proton mail conversations list --keyword "parking permit" --folder all
proton mail messages export --keyword "parking permit" --folder all --all --dest-dir ./trail
```

With an index, `--from` and `--to` match display names as well as addresses. Proton's search operators - `invoice | receipt`, `book*`, `!word` - stop applying: every term is matched literally, and a quoted phrase is one term.

A quoted reply is indexed without the history it quotes, so a keyword matches the message that says it rather than every later message in the thread.

While bodies are still downloading, a keyword search says what it did not read:

```console
$ proton mail messages list --keyword "parking permit" --folder all
No messages match.
! Only 12400 of 48213 message bodies are indexed, newest first, so older mail was searched by everything but its text. `proton index create mail` continues the download.
```

## Find a file without a walk

A Drive filter opens every folder in its scope. With an index, the same listing is answered from the copy on this machine:

```bash
proton index create drive
proton drive items list --pattern "*.pdf" --recursive
```

The rows are the rows the walk gives, and a file uploaded since the build is in them. A filtered listing shows each item's full path. Your own files are indexed; a public link, a share somebody sent you and a computer's backup are read from Proton as before.

## Search inside files

Drive is indexed in two passes. The first takes the tree, which is minutes. The second downloads the text of your text files, most recently changed first:

```console
$ proton index create drive
Indexing drive        ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━  100%  8120 items  in 41s
Indexing drive texts  ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━  100%  1240 / 1240 texts  in 4m
✓ Indexed 8120 items.
```

Text files are the ones Drive opens as text: `.txt`, `.md`, `.csv`, `.json`, `.html`, `.xml`, and source code, up to 1 MB each. A PDF, an image, an SVG, an archive and anything larger are indexed by name alone. Files in the trash are left out.

`--keyword` then matches names and contents, everywhere the Drive filters are taken:

```bash
proton drive items list /Notes --keyword "parking permit" --recursive
proton drive items trash --scope /Notes --keyword DRAFT --recursive --dry-run
```

Every term has to appear, case and accents are ignored, and a quoted phrase is one term.

A keyword says what it was not able to read:

```console
$ proton drive items list /Notes --keyword "parking permit" --recursive
No items match.
! Only 512 of 1240 files have their text indexed, newest first, so the rest were searched by name alone. `proton index create drive` continues the download.
```

Without a drive index, and inside `--computer`, `--shared` or `--link`, a keyword matches names and says so.

## Find an event by what it says

`--keyword` matches the title, the location, the description, the organizer and the people invited. On its own it covers every event there has been and the next three years of every repeating one:

```bash
proton calendar events list --keyword dentist
proton calendar events list --keyword "team offsite" --after 2026-01-01 --before 2026-12-31
```

It works without an index by reading your calendars first, which takes a moment. `proton index create calendar` makes it instant.

## See what is indexed

```console
$ proton index list
APP       INDEXED                         UNREADABLE  UPDATED           SIZE
────────  ──────────────────────────────  ──────────  ────────────────  ───────
calendar  1342 events                                 2026-04-15 14:30  812.0 KB
drive     8120 items, 512 of 1240 texts               2026-04-15 14:31  3.4 MB
mail      48213 messages, 12400 bodies    31          2026-04-15 14:32  36.2 MB
3 indexes.
```

For mail and drive, INDEXED names both passes: every message is indexed, and 12400 of them hold the body a keyword reads; the whole tree is indexed, and 512 of the 1240 files that are text hold their text. A first pass that has not finished reads `12400 of 48213 messages` instead.

`UNREADABLE` counts things whose contents would not open - a message body, an event's text, a file's name or bytes. They are searchable by everything else they carry.

This reads files, so it works signed out.

## Keep it current

A search brings the index up to date before it answers. Two ways to have that done already:

```bash
proton index update
proton index watch
```

`update` catches up once, for cron or a systemd timer. `watch` stays attached and applies changes as they land; while it runs, a search reads the copy as of the last poll, at most 30 seconds old. Prefer `watch` when searches are frequent, and `update` on a timer when they are not. Both finish a build that was interrupted, and neither indexes an app that has no index; `create` does that. For running `watch` unattended, see [Scripting](../using/scripting.md#run-a-watch-under-systemd).

When Proton cannot say what has changed since the last catch-up - after a long gap, or a change across the whole account - the next `create` or `update` reads the app again and settles it. The same happens when part of the copy on this machine would not read back, and when a password reset has put your files on a new volume. A mail copy made by a version of proton that recorded less about each message is read again the same way, keeping its bodies. Until then a search says so:

```console
$ proton mail messages list --unread --folder all
! The mail index has fallen behind Proton and was searched as it stands. `proton index update` catches it up.
```

## Remove it

```console
$ proton index delete mail
Would delete index "mail" (142.3 MB). This cannot be undone. Continue? [y/N] y
✓ Deleted index "mail" (142.3 MB).
```

Searches go back to what Proton answers. `proton account profiles delete` and `proton uninstall --purge` remove indexes too.

## What is on disk

The copy lives in `~/.config/proton-cli/index/`, one directory per profile, readable only by you. Everything it holds - a subject, a body, a file's name, what an event says - is encrypted with your account's keys. What that protects and what it does not: [Security](../about/security.md#what-is-stored-on-disk).
