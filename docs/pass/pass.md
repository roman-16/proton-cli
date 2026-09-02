# proton pass

Vaults, logins and secrets.

Every command under `proton pass`, with the arguments and flags it takes. For these commands in use, see [the pass guide](README.md).

Holds `aliases`, `breaches`, `export`, `generate`, `import`, `invitations`, `items`, `links`, `settings`, `shared`, `sharing`, `trash` and `vaults`.

## `export`

Write the vaults you own out as a Proton Pass archive, or to stdout with --dest -.

This writes the same archive format Proton Pass writes, so the app can read it back.

Give a passphrase and the contents are encrypted to it. Without one, the archive holds every password in the clear.

Only vaults you own are included. A vault somebody shared with you is theirs to back up.

```
proton pass export
```

```bash
proton pass export --dest pass-backup.zip --passphrase-file ~/.backup-passphrase
proton pass export --dest pass-backup.zip
```

| Flag | Description |
| --- | --- |
| `--dest string` | Write to this path, or - for stdout |
| `--dest-dir string` | Write into this directory, keeping each item's own name |
| `--force` | Overwrite a file that already exists |
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

Read a Proton Pass archive back in, or one on stdin with -.

A vault in the file lands in the vault of that name, which is created if it does not exist.

Items are always added, never matched against what is already there, so reading the same file twice creates duplicates.

```
proton pass import PATH
```

```bash
proton pass import pass-backup.zip --passphrase-file ~/.backup-passphrase
proton pass import --dry-run pass-backup.zip
```

| Flag | Description |
| --- | --- |
| `--passphrase-file string` | Read the passphrase that locks the file from a file |
| `--passphrase-stdin` | Read the passphrase that locks the file from stdin |

---

Every command also takes the [flags that work everywhere](../about/commands.md#flags-that-work-on-every-command).
