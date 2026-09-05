# Filters and bulk changes

Every command that can act on many things takes the same two ways of saying which: name them, or describe them. Give both and you get the union.

```bash
proton mail messages trash 5bH2mQxK 9xL4pQrT              # named
proton mail messages trash --from newsletter@example.com --older-than 90d   # described
proton mail messages trash 5bH2mQxK --unread --folder spam                  # both
```

## The filters

| Filter | What it matches |
| --- | --- |
| `--folder` · `--scope` · `--vault` | Where to look |
| `--older-than` · `--newer-than` | By age |
| `--larger-than` · `--smaller-than` | By size |
| `--pattern` | The name, against a glob |
| `--unread` · `--starred` · `--type` | By state or kind |
| `--all` | Everything in scope, rather than a subset |
| `--limit` | How many a bulk verb may affect |

Each command's own page lists the filters it takes. Mail adds `--from`, `--to`, `--subject`, `--keyword`, `--after` and `--before`.

## Check a selection before acting on it

`list` takes the same filters as the verbs beside it, so you can see what matches first:

```bash
proton drive items list /Build --pattern "*.tmp" --recursive     # see what matches
proton drive items trash --scope /Build --pattern "*.tmp" --recursive   # act on it
```

`list` takes a path as its argument. Bulk verbs take `--scope` instead, because their arguments name individual items.

You can also preview the change itself with [`--dry-run`](confirmations.md#preview-a-change-with---dry-run).

## Something has to be selected

A command needs at least one reference or one filter:

```console
$ proton mail messages trash
Error: Nothing selected.
Try:   pass a REF, or a filter such as --unread, --starred, --from or --older-than.
       Use --all to target a whole folder.
```

## Limits

`--limit` defaults to 150 in Mail: a guard against a mistyped filter, not a technical bound. A selection that fills it says so, `--limit N` reads as far as it takes to find N, and `--limit 0` lifts the cap altogether.

Bulk verbs act on the IDs the selection resolved to, in batches of fifty, and report Proton's answer per item. Whatever was refused is named, and the count reports what actually landed.

A filter that matches a folder and the files inside it selects the folder alone, because every verb acts on a folder whole.

**Drive filters run on your machine.** `--pattern` and the size and age filters walk the tree folder by folder, because Drive's index is built by the web client rather than by the server. Over a large tree that is one request per folder, so narrow it with `--scope`.
