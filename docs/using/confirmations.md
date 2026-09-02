# Dry runs and confirmations

Every command that changes something can show you what it would do first. Some commands stop and ask before they act. You can make more of them ask, or refuse outright.

## Preview a change with `--dry-run`

`--dry-run` works on every command that changes state. It resolves references, applies filters, and shows you the rows themselves:

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

## Which commands ask before they act

proton asks in two cases: before it removes something it cannot put back, and before it removes things you did not name yourself. Nothing else stops to ask.

| | You named it | A filter found it |
| --- | --- | --- |
| `delete` · `empty` · `uninstall` | asks | asks |
| `trash` | acts | asks |
| everything else | acts | acts |

The question shows the things themselves, never just a count:

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

Anything but a plain `y` means no, including pressing Enter.

[Why these two cases and no others](../about/why.md#why-it-asks-before-some-removals-and-not-others).

## In a script

A script has nobody to ask, so the question becomes an error and nothing is removed. `--yes` answers it in advance.

```console
$ proton mail messages delete --folder spam --older-than 30d
Error: Would delete 112 messages. This cannot be undone.
Try:   --yes to confirm, or --dry-run to see what it would touch.
```

## Making more commands ask

The table above is the floor. It holds with no configuration at all. On top of it you can ask proton to stop for more, or to refuse outright. This is useful when something other than you is typing the commands.

### Writing a policy

In [the config file](settings.md#the-config-file):

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

### The scope

A scope is the start of a command, written as you would type it but without `proton`. Stop wherever you like: at an app, at a collection, or at one whole command.

| Scope | What it covers |
| --- | --- |
| `*` | Every command |
| `mail` | Everything under `proton mail` |
| `mail drafts` | Every verb on drafts: `list`, `create`, `update`, `delete`, `send` |
| `mail drafts send` | That one command and nothing else |

In the file, the scope is the key, so a multi-word scope keeps its spaces: `mail drafts send: all`. On one line it comes before the colon: `mail drafts send:all`.

A scope that names no command is refused when the file loads:

```console
$ proton --confirm 'mail lettuce:all' mail messages list
Error: "mail lettuce" is not a command, so it cannot be a confirmation scope.
Try:   mail
```

### The class

| Class | Commands it covers |
| --- | --- |
| `reads` | Anything that does not change state |
| `mutations` | Anything that does |
| `deletions` | `delete`, `empty`, `uninstall` |
| `all` | Every command |
| `default` | Nothing beyond the table above, so a narrower scope can opt out of a broader directive |

### Exceptions

The narrowest scope that mentions a command decides, so a broad rule can take exceptions:

```bash
# every change asks, except in Drive, where the usual rules apply
PROTON_CONFIRM='mutations, drive:default'

# reading anything asks, except the one listing a script runs constantly
PROTON_CONFIRM='reads, mail messages list:default'
```

That holds **within** one place a policy is written. Between the file, the variable and the flag it does not: each is weighed on its own and the strictest wins. An exception written in one of them cannot stand down a rule written in another.

## Ask

```console
$ proton mail drafts send 7fK2mQ
Would send 1 message. Continue? [y/N]
```

A command that changes nothing has no filter to resolve and nothing to count, so it names itself:

```console
$ proton pass items list
Would run proton pass items list. Continue? [y/N]
```

In a script the question becomes an error, and `--yes` answers it in advance.

## Deny

```console
$ proton mail messages delete 5bH2xR9t --yes
Error: Deleting is turned off by your confirmation policy.
```

The exit code is `6`. Nothing answers a deny: not `--yes`, not `--dry-run`, and not a `--confirm` on the command line. To lift it, edit the file that declared it.

**A deny is not a security boundary.** It guards against a command run carelessly. Anything that can edit your config file can remove it. [The reasoning](../about/why.md#why-the-confirmation-policy-resolves-the-other-way).
