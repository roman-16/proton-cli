# How a command is built

Every proton command has the same shape. Learn it once and you can guess most of the rest.

```
proton <app> <collection> <verb> [TARGET...] [--flags]
```

```bash
proton mail messages list
proton drive items move /report.pdf --into /Archive
proton pass items get github.com
proton calendar events create --title Standup --start 2026-04-16T09:00
```

- **App** - `mail`, `drive`, `calendar`, `pass`, `contacts`, `account`, or `api`.
- **Collection** - the kind of thing you are working with: `messages`, `items`, `events`, `vaults`.
- **Verb** - what to do: `list`, `get`, `create`, `send`.
- **Target** - which thing. See [Naming what to act on](using/naming.md).

A group on its own never does anything. `proton mail settings` prints help; `proton mail settings get` shows your settings.

## The verbs

Each word means one thing, everywhere it appears.

| Verb | What it does |
| --- | --- |
| `list` | Show a collection |
| `get` | Show one thing in full |
| `create` · `update` · `delete` | Make, change, remove permanently |
| `trash` · `restore` · `empty` | Remove reversibly, put back, clear out |
| `move --into` · `copy --into` | Put into another container |
| `upload` · `download` | Move bytes to or from your disk |
| `export` · `import` | Write documents to disk, or read them back |
| `send` · `reply` · `forward` | Mail going out |
| `label` · `unlabel` · `star` · `unstar` | Attach or detach |
| `enable` · `disable` | Turn something on or off |
| `add` · `remove` | Put a member into a container, or take one out |
| `accept` · `decline` | Answer an invitation |
| `set` | Write one setting |
| `login` · `logout` | Your session |

To rename anything, use `update --name`. There is no `rename` verb.

## Flags that mean the same thing everywhere

| Flag | Always means |
| --- | --- |
| `--to` | An email recipient, or a filter matching one |
| `--into` | A container on Proton's side: a folder, a vault, a calendar |
| `--dest` | A path on your disk |
| `--force` | Overwrite a local file that already exists |
| `--all` | Everything in scope, rather than a subset |

```bash
proton mail messages send --to alice@proton.me       # a recipient
proton mail messages list --to alice@proton.me       # matching a recipient
proton mail messages move REF --into archive         # a container over there
proton drive items download /report.pdf --dest .     # a path over here
```

Five flags have a single-letter form: `-p` profile, `-o` output, `-n` dry run, `-q` quiet, `-y` yes. They cluster, so `-qn` is a quiet dry run.

## Saying none

Removing an optional value takes one of three spellings, and each flag's help says which:

| Spelling | Used when | Example |
| --- | --- | --- |
| The value itself | The word is one the flag already prints | `--expires never` |
| `--no-x` | The flag names a state | `calendar events update REF --no-remind` |
| `--clear-x` | The value cannot carry the word: text, or a password read from a file | `--clear-signature`, `--clear-link-password` |

## Getting help

Every command documents itself and links to its own page:

```console
$ proton mail messages send --help
Compose and send a message
…
Global flags:    proton --help
Full reference:  https://proton-cli.lerchster.dev/mail/messages/#send
```

`proton help mail messages send` shows the same screen.

The page a command links to is named after the command. `proton drive items upload` is documented at `/drive/items/#upload`.

Shell completion knows the whole tree, including which values each flag accepts. See [Install](install.md#shell-completions).

## What to read next

| Page | What it covers |
| --- | --- |
| [Naming what to act on](using/naming.md) | IDs, short IDs, names, paths |
| [Filters and bulk changes](using/filters.md) | Acting on many things at once |
| [Dry runs and confirmations](using/confirmations.md) | Previewing a change, and what asks first |
| [Output and exit codes](using/output.md) | What comes back, and what a failure means |
| [All commands](about/commands.md) | Every command in one table |
