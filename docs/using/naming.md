# Naming what to act on

Wherever a command's usage shows `REF`, you can name the thing in four ways. Use whichever you already have.

```bash
proton mail messages get 5bH2mQxKT9wLpN4v…    # the full ID
proton mail messages get 5bH2mQxK             # the short ID a list printed
proton mail messages get "Invoice #2291"      # the subject
proton contacts get jane                      # a name or an address
```

If nothing matches, the command exits `3`. If more than one thing matches, it prints the candidates and exits `4`:

```console
$ proton contacts get jane
Error: "jane" matches 2 contacts.
Try:   narrow the term, or use one of:
         7Kd91mQx  jane@example.com
         3Ns8pT2v  jane.roe@work.example
```

## Short IDs

On a terminal, lists shorten Proton's IDs to eight characters and remember what they showed you. Paste one straight back into the next command.

Short IDs carry no ellipsis and never start with a dash, so they copy cleanly out of a table and your shell reads them as-is.

**In scripts, IDs are always full.** Pipes, redirects, `--output json` and `--output yaml` all emit complete IDs, so no script ever sees a truncated value. To turn shortening off interactively too, pass `--full-ids`.

**A short ID only works on the machine that printed it.** The lookup table lives in `~/.config/proton-cli/idcache/<profile>.json`. If you copied one from elsewhere, run the matching `list` here first, or use the full ID.

## Tab completion

With [completion installed](../install.md#shell-completions), a `REF` completes from what your listings showed you - the short ID, and the subject, name or address beside it.

```console
$ proton mail conversations list
ID        FROM                       SUBJECT                                MESSAGES  DATE
ketTSogw  Gastronaut Reservierungen  Reservation Confirmation - Vero Sushi          1  2026-08-31 19:31

$ proton mail conversations get ket⇥
ketTSogw  -- Reservation Confirmation - Vero Sushi
```

The subject completes too, because it is a reference just as much as the ID is. Your shell decides between them by what you have already typed:

```console
$ proton mail messages get Invo⇥
Invoice #2291 is ready  -- 5bH2mQxK

$ proton contacts get jane⇥
Jane Doe          -- QmxLp2Rt
jane@example.com  -- QmxLp2Rt
```

**It offers only what this machine has seen.** Completion reads the same table short IDs come from, so it never waits on Proton - and a collection you have not listed yet has nothing to offer, and says which listing would fill it:

```console
$ proton pass items get ⇥
Nothing seen yet - run `proton pass items list` first
```

## Two IDs in one

A Pass item and a calendar event each need two IDs, written as one slash-separated token. Lists print them this way, and you paste them back the same way. Short IDs work on both halves at once.

```bash
proton pass items get SHARE_ID/ITEM_ID
proton calendar events get CALENDAR_ID/EVENT_ID
```

## One occurrence of a recurring event

A recurring event is stored once and happens many times. To name a single occurrence, add its own start time after an `@`.

```bash
proton calendar events get 4f2a1b9c@2026-04-16T09:00   # one occurrence
proton calendar events get 4f2a1b9c                    # the whole series
```

Keep the `@` part to act on that occurrence. Drop it to act on the series. Add `--onwards` to act on one occurrence and every later one.

## Drive uses paths

Files and folders are named by their path:

```bash
proton drive items get /Documents/report.pdf
proton drive items move /Documents/report.pdf --into /Archive
```

Some things in Drive have no place in the tree, so they have no path: a trashed item, a photo, an album. Name those by the `REF` their list showed.

```bash
proton drive trash restore 7Kd91mQx
proton drive photos download 3Ns8pT2v --dest-dir ./photos
```

## Full IDs that start with a dash

Proton's IDs are base64, and `-` is one of its characters, so about one ID in sixty-four starts with a dash. Paste them like any other reference:

```bash
proton pass items get -x76EpiVSJf2oHzHgyC2D_jF8O…==/_fb26gvMWjnM7US4_wpTNm_LqI…==
proton contacts delete -bJxDLEMvt-Z6t4Yna7V8SYQ_F…==
```

**Put your flags before such an ID.** Everything after it is read as another argument.

```bash
proton mail messages attachments download --dest-dir ./files -bJxDLEMvt-…==   # works
proton mail messages attachments download -bJxDLEMvt-…== --dest-dir ./files   # does not
```

Short IDs need none of this. Their eight characters begin after any leading dashes, so flags can go on either side.

## Next

- [Filters and bulk changes](filters.md) - naming many things at once
- [Dry runs and confirmations](confirmations.md) - checking a selection before it happens
