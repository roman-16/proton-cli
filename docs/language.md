# The language

proton has one grammar. Learn it once and you can guess the rest.

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

proton asks before it removes something it cannot put back, before any filter-selected change, and before a change communicates externally, changes network connectivity, or changes security-sensitive state.

| | you named it | a filter found it |
| --- | --- | --- |
| `delete` · `empty` · `uninstall` | asks | asks |
| `trash` | just does it | asks |
| send · share · session · network/security changes | asks | asks |
| other reversible changes | just does it | asks |

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

Anything but a plain `y` means no, including pressing enter. ([Why these boundaries](design-notes.md#why-it-asks-before-some-changes).)

**In a script** there is nobody to ask, so the question becomes an error and nothing changes. `--yes` is the answer given in advance:

```console
$ proton mail messages delete --folder spam --older-than 30d
Error: Would delete 112 messages. This cannot be undone.
Try:   --yes to confirm, or --dry-run to see what it would touch.
```

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

Next: [naming the thing you want](references.md), and [what comes back](output.md).
