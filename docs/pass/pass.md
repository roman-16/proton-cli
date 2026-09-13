# proton pass

Vaults, logins and secrets.

Every command under `proton pass`, with the arguments and flags it takes. For these commands in use, see [the pass guide](README.md).

Holds `aliases`, `breaches`, `export`, `generate`, `import`, `invitations`, `items`, `links`, `settings`, `shared`, `sharing`, `trash` and `vaults`.

## `export`

Write the vaults you own out to a file, or to stdout with --dest -.

--format zip writes the archive Proton Pass reads back, with the attachments in it. --format json writes the document that archive holds, without them. --format csv writes a spreadsheet, which leaves out custom fields, attachments, passkeys and the keys of SSH items.

Give a passphrase and the items are encrypted to it. Without one, the file holds every password in the clear. A CSV cannot be encrypted, and attachments never are.

An archive includes the attachments; --no-attachments leaves them out, which is much faster.

Only vaults you own are included. A vault somebody shared with you is theirs to back up.

```
proton pass export
```

```bash
proton pass export --dest pass-backup.zip --passphrase-file ~/.backup-passphrase
proton pass export --dest pass-backup.zip
proton pass export --dest pass-backup.zip --no-attachments
proton pass export --format csv --dest pass.csv
proton pass export --format json --dest -
```

| Flag | Description |
| --- | --- |
| `--dest string` | Write to this path, or - for stdout |
| `--dest-dir string` | Write into this directory, keeping each item's own name |
| `--force` | Overwrite a file that already exists |
| `--format string` | How to lay the items down: zip, csv, json (default `zip`) |
| `--no-attachments` | Leave attachments out, which is much faster |
| `--passphrase-file string` | Read the passphrase that locks the file from a file |
| `--passphrase-stdin` | Read the passphrase that locks the file from stdin |

## `generate`

Make a password, without storing it anywhere.

Runs locally. It reaches no account and needs no session.

The alphabet leaves out i, o, l and their capitals, which are easily misread. They are used only when letters are all the password may contain.

Every character kind you ask for is guaranteed to appear at least once.

--words makes a passphrase instead, from Proton's own wordlist. Each word is capitalised and followed by a digit, unless --no-uppercase or --no-digits says otherwise.

```
proton pass generate
```

```bash
proton pass generate
proton pass generate --length 32
proton pass generate --no-symbols --length 24
proton pass generate --words 4
proton pass generate --words 4 --separator space --no-digits
```

| Flag | Description |
| --- | --- |
| `--length int` | How many characters (default `20`) |
| `--no-digits` | Leave the digits out |
| `--no-symbols` | Leave the symbols out |
| `--no-uppercase` | Leave the capitals out |
| `--separator string` | What stands between the words of a passphrase: comma, digit, hyphen, period, space, symbol, underscore (default `hyphen`) |
| `--words int` | Make a passphrase of this many words instead |

## `import`

Read items in from a password manager's export, or one on stdin with -.

--manager names the program that wrote the file, and defaults to Proton Pass: its archive, the document inside one, or its CSV. Each other value reads what that program exports - 1Password .1pux, .1pif or .zip, Bitwarden .json or .zip, Dashlane .csv or .zip, KeePass .xml, Kaspersky .txt, and CSV or JSON for the rest.

A vault, folder or group in the file lands in the vault of that name, which is created if it does not exist. Items in none land in your first vault. --vault puts everything into the one you name.

Items are always added, never matched against what is already there, so reading the same file twice creates duplicates.

Attachments in the file are put back on their items, which needs a paid Pass plan. Whatever cannot be put back is named once the items have landed.

```
proton pass import PATH
```

```bash
proton pass import pass-backup.zip --passphrase-file ~/.backup-passphrase
proton pass import --dry-run pass-backup.zip
proton pass import bitwarden-export.json --manager bitwarden
proton pass import chrome-passwords.csv --manager chrome --vault Personal
```

| Flag | Description |
| --- | --- |
| `--manager string` | The password manager that wrote PATH: 1password, apple-passwords, bitwarden, brave, chrome, dashlane, edge, enpass, firefox, kaspersky, keepass, keeper, lastpass, nordpass, proton-pass, roboform, safari (default `proton-pass`) |
| `--passphrase-file string` | Read the passphrase that locks the file from a file |
| `--passphrase-stdin` | Read the passphrase that locks the file from stdin |
| `--vault string` | Put everything into this vault |

---

Every command also takes the [flags that work everywhere](../about/commands.md#flags-that-work-on-every-command).
