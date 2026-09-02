# How commands read

proton-cli has one grammar: `proton <app> <collection> <verb>`. Learn it once and you can guess the rest of the two hundred commands, name the thing you want in whatever way you already know it, and see what a bulk change would touch before it happens.

```
proton <app> <collection> <verb> [TARGET…] [--flags]
```

```bash
proton mail messages list
proton drive items move /report.pdf --into /Archive
proton pass items get github.com
proton calendar events create --title Standup --start 2026-04-16T09:00
```

A **group** never does anything itself - `proton mail settings` prints help, `proton mail settings get` shows your settings. Every command that acts is named by a verb.

## Verbs

One word per idea, everywhere it appears.

| Verb | Means |
| --- | --- |
| `list` · `get` | show a collection, or one thing in full |
| `create` · `update` · `delete` | the usual three |
| `trash` · `restore` · `empty` | remove reversibly, put back, clear out |
| `move --into` · `copy --into` | put into another container |
| `upload` · `download` | move bytes to or from your disk |
| `export` · `import` | documents to or from disk |
| `send` · `reply` · `forward` | mail going out |
| `label` · `unlabel` · `star` · `unstar` | attach or detach |
| `enable` · `disable` | turn a thing on or off |
| `add` · `remove` | put a member into a container, or take one out |
| `accept` · `decline` | answer an invitation |
| `set` | write one setting |
| `login` · `logout` | your session |

To rename anything, use `update --name`. The [command reference](commands/README.md) has the full list.

## Naming the thing you want

Wherever a command's usage shows `REF`, four things work.

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

### Short IDs

On a terminal, lists shorten Proton's IDs to eight characters and remember what they showed you, so you can paste one straight back. They carry no ellipsis and never start with a dash, so they copy cleanly out of a table and a shell reads them as what they are.

Pipes, redirects and `--output json` always emit **full** IDs, so no script ever sees a truncated value. `--full-ids` switches shortening off interactively too.

A short ID only resolves on the machine that printed it - the lookup table lives in `~/.config/proton-cli/idcache/<profile>.json`. Copied one from elsewhere? Run the matching `list` here first, or use the full ID.

### Two IDs in one

A Pass item and a calendar event each need two IDs, written as one slash-separated token. Lists print them this way and you paste them back the same way. Short IDs work on both halves at once.

```bash
proton pass items get SHARE_ID/ITEM_ID
proton calendar events get CALENDAR_ID/EVENT_ID
```

A **recurring** event is stored once and happens many times, so naming a single occurrence takes one more part: its own start, after an `@`.

```bash
proton calendar events get 4f2a1b9c@2026-04-16T09:00   # one occurrence
proton calendar events get 4f2a1b9c                    # the whole series
```

Keep the `@` part and you act on that occurrence; drop it and you act on the series. `--onwards` widens one occurrence to it and every later one.

### Drive is addressed by path

Files and folders are named by their path:

```bash
proton drive items get /Documents/report.pdf
proton drive items move /Documents/report.pdf --into /Archive
```

Something with no place in the tree - a trashed item, a photo, an album - has no path, so it is named by the `REF` its list showed:

```bash
proton drive trash restore 7Kd91mQx
proton drive photos download 3Ns8pT2v --dest-dir ./photos
```

### Full IDs that start with a dash

Proton's IDs are base64, `-` is one of its characters, and so about one in sixty-four begins with one. Paste them like any other reference - the dash is part of the ID and is handled for you:

```bash
proton pass items get -x76EpiVSJf2oHzHgyC2D_jF8O…==/_fb26gvMWjnM7US4_wpTNm_LqI…==
proton contacts delete -bJxDLEMvt-Z6t4Yna7V8SYQ_F…==
```

One rule comes with it: **put your flags before the ID**, because everything after such an ID is read as another argument.

```bash
proton mail messages attachments download --dest-dir ./files -bJxDLEMvt-…==   # yes
proton mail messages attachments download -bJxDLEMvt-…== --dest-dir ./files   # no
```

Short IDs need none of this: the eight characters begin after any leading dashes, so flags can go on either side.

## Saying which ones

Every command that can act on many things takes the same two ways of saying which: name them, or describe them. Both at once is a union.

```bash
proton mail messages trash 5bH2mQxK 9xL4pQrT
proton mail messages trash --from newsletter@example.com --older-than 90d
proton mail messages trash 5bH2mQxK --unread --folder spam
```

| Filter | Means |
| --- | --- |
| `--folder` · `--scope` · `--vault` | where to look |
| `--older-than` · `--newer-than` | by age |
| `--larger-than` · `--smaller-than` | by size |
| `--pattern` | match the name against a glob |
| `--unread` · `--starred` · `--type` | by state or kind |
| `--all` | everything in scope, rather than a subset |
| `--limit` | cap how many a bulk verb affects |

`list` takes the same filters as the verbs beside it, so you can see a selection before acting on it:

```bash
proton drive items list /Build --pattern "*.tmp" --recursive   # see what matches
proton drive items trash --scope /Build --pattern "*.tmp" --recursive
```

What `list` says with its `PATH` argument, a bulk verb says with `--scope`, because it uses its arguments to name things instead.

A command needs at least one reference or filter:

```console
$ proton mail messages trash
Error: Nothing selected.
Try:   pass a REF, or a filter such as --unread, --starred, --from or --older-than.
       Use --all to target a whole folder.
```

## When it asks first

proton asks before it removes something it cannot put back, and before it removes things you did not name. Nothing else ever stops to ask.

| | you named it | a filter found it |
| --- | --- | --- |
| `delete` · `empty` · `uninstall` | asks | asks |
| `trash` | just does it | asks |
| everything else | just does it | just does it |

The question shows the things themselves, never a count:

```console
$ proton mail messages delete --from newsletter@example.com --older-than 90d
Would delete 3 messages:

ID        FROM          SUBJECT              DATE
────────  ────────────  ───────────────────  ────────────────
hR8sT2vW  Example News  January round-up     2026-01-08 06:00
kM4nP9qL  Example News  December round-up    2025-12-08 06:00
zC7bX1yE  Example News  November round-up    2025-11-08 06:00

This cannot be undone. Continue? [y/N]
```

Anything but a plain `y` means no, including pressing enter. ([Why these two cases](design-notes.md#why-it-asks-before-some-removals-and-not-others).)

**In a script** there is nobody to ask, so the question becomes an error and nothing is removed. `--yes` is the answer given in advance:

```console
$ proton mail messages delete --folder spam --older-than 30d
Error: Would delete 112 messages. This cannot be undone.
Try:   --yes to confirm, or --dry-run to see what it would touch.
```

## Asking about more than that

The table above is the floor, and it holds with no configuration at all. On top of it you can ask proton to stop for more, or to refuse outright - useful when something other than you is typing the commands.

### Writing a policy

In [the config file](configuration.md#the-config-file):

```yaml
confirm:
  ask:
    "*": mutations
    pass: all
  deny:
    "*": deletions
    drive: all
    mail drafts send: all
```

Or on one line, for a shell or a CI job:

```bash
export PROTON_CONFIRM='mutations, pass:all, deletions=deny, drive:all=deny, mail drafts send:all=deny'
```

A directive is `[scope:]class[=deny]`.

**The scope is the start of a command**, written the way you would type it but without `proton`. Stop wherever you want to stop: at an app, at a collection inside it, or at one whole command.

| Scope | What it reaches |
| --- | --- |
| `*` | every command |
| `mail` | everything under `proton mail` |
| `mail drafts` | every verb on drafts - `list`, `create`, `update`, `delete`, `send` |
| `mail drafts send` | that one command and nothing else |

In the file the scope is the key, so a scope of several words is written with the spaces in it: `mail drafts send: all`. On one line it comes before the colon: `mail drafts send:all`.

A scope that names no command is refused when the file loads, rather than sitting there guarding nothing:

```console
$ proton --confirm 'mail lettuce:all' mail messages list
Error: "mail lettuce" is not a command, so it cannot be a confirmation scope.
Try:   mail
```

**The class is one of five:**

| Class | Commands it covers |
| --- | --- |
| `reads` | anything that does not change state |
| `mutations` | anything that does |
| `deletions` | `delete`, `empty`, `uninstall` |
| `all` | every command |
| `default` | nothing beyond the table above - how a narrower scope opts out of a broader directive |

**The narrowest scope that mentions a command is the one that decides**, so a broad rule takes exceptions:

```bash
# every change asks, except in Drive, where the usual rules apply
PROTON_CONFIRM='mutations, drive:default'

# reading anything asks, except the one listing a script runs constantly
PROTON_CONFIRM='reads, mail messages list:default'
```

That holds within one place a policy is written. Between the file, the variable and the flag it does not: each is weighed on its own and the most cautious of them wins, so an exception written in one of them cannot stand down a rule written in another.

### Ask

```console
$ proton mail drafts send 7fK2mQ
Would send 1 message. Continue? [y/N]
```

A command that changes nothing has no filter to resolve and nothing to count, so it names itself:

```console
$ proton pass items list
Would run proton pass items list. Continue? [y/N]
```

In a script, as with the built-in cases, the question becomes an error that `--yes` answers in advance.

### Deny

```console
$ proton mail messages delete 5bH2xR9t --yes
Error: Deleting is turned off by your confirmation policy.
```

Exit code `6`. Nothing answers a deny - not `--yes`, not `--dry-run`, and not a `--confirm` on the command line. Lifting it means editing the file that declared it.

That is what makes it worth having, and also the whole of what it is: a guard against a command run carelessly, not a security boundary. Anything that can edit your config file can remove it ([the reasoning](design-notes.md#why-the-confirmation-policy-resolves-the-other-way)).

## Dry runs

Every command that changes something takes `--dry-run`. It resolves references, applies filters, and shows you the things themselves:

```console
$ proton mail messages trash --from newsletter@example.com --older-than 90d --dry-run
Dry run - would move 3 messages to trash:

ID        FROM          SUBJECT              DATE
────────  ────────────  ───────────────────  ────────────────
hR8sT2vW  Example News  January round-up     2026-01-08 06:00
kM4nP9qL  Example News  December round-up    2025-12-08 06:00
zC7bX1yE  Example News  November round-up    2025-11-08 06:00
```

Dry-run output goes to stderr, so it still appears if you redirect stdout.

## Getting help

Every command documents itself, and completion knows the whole tree - including which values each constrained flag accepts.

```bash
proton --help
proton mail messages send --help
```

Next: [what comes back](output.md), and the page for the app you want - [Mail](apps/mail.md), [Drive](apps/drive.md), [Calendar](apps/calendar.md), [Pass](apps/pass.md) or [Contacts](apps/contacts.md).
