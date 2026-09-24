# proton account keys

The keys your data is encrypted with.

Account keys belong to the account. Address keys belong to one address each: its primary key is what mail to it is encrypted to and what it signs with, and its other keys open and verify what arrived before.

Every command under `proton account keys`, with the arguments and flags it takes. For these commands in use, see [the account guide](README.md).

Holds `create`, `delete`, `export`, `get`, `import`, `list`, `reactivate` and `update`.

## `create`

Generate a key for an address, as its primary key.

REF is the address. The key it replaces stays, and goes on opening and verifying what arrived before. An account that creates post-quantum keys is refused: generate the key in a Proton client.

Asks for your password even when you are signed in. With no terminal to ask, pass --password-file, which takes - for stdin.

```
proton account keys create REF
```

```bash
proton account keys create alice@proton.me
proton account keys create alice@proton.me --dry-run
```

| Flag | Description |
| --- | --- |
| `--password-file string` | Read the account password from a file, or - for stdin |
| `--totp string` | Two-factor code |

## `delete`

Delete keys of an address.

Nothing sealed to a deleted key opens again, and what it signed is no longer verified. `proton account keys export REF --private` keeps a copy first. The primary key of an address and the account's own keys cannot be deleted.

Asks for your password even when you are signed in. With no terminal to ask, pass --password-file, which takes - for stdin.

```
proton account keys delete REF...
```

```bash
proton account keys delete 7Hn2Lw0R
proton account keys delete 7Hn2Lw0R --yes
```

| Flag | Description |
| --- | --- |
| `--password-file string` | Read the account password from a file, or - for stdin |
| `--totp string` | Two-factor code |

## `export`

Write keys out to files, or one to stdout with --dest -.

A file is named publickey.OWNER-FINGERPRINT.asc, or privatekey.OWNER-FINGERPRINT.asc with --private, where OWNER is the address or the account's username.

--private writes the private key, locked with a passphrase of at least eight characters, and asks for your password even when you are signed in. With no terminal to ask, pass --password-file and --passphrase-file. Only one of them can be -.

```
proton account keys export REF...
```

```bash
proton account keys export 7Hn2Lw0R
proton account keys export 7Hn2Lw0R --dest -
proton account keys export 7Hn2Lw0R --private --dest-dir ~/backup
proton account keys export 7Hn2Lw0R --private --password-file /run/secrets/proton --passphrase-file /run/secrets/key-backup
```

| Flag | Description |
| --- | --- |
| `--dest string` | Write to this path, or - for stdout |
| `--dest-dir string` | Write into this directory, keeping each item's own name |
| `--force` | Overwrite a file that already exists |
| `--passphrase-file string` | Read the passphrase that locks the file from a file, or - for stdin |
| `--password-file string` | Read the account password from a file, or - for stdin |
| `--private` | Export the private key, locked with a passphrase |
| `--totp string` | Two-factor code |

## `get`

Show one key and what it is used for.

REF is the key's ID, short ID or fingerprint.

```
proton account keys get REF
```

```bash
proton account keys get 7Hn2Lw0R
proton account keys get c4e1a86d0f3b7e25a9c1d4f8b6e2a0937d5c1e84
```

## `import`

Add keys to an address from files, or from stdin with -.

REF is the address. A key joins it as one that reads, or as its primary key when it has no other. A copy of a key the address holds locked comes back as that key. A key's public half is published with every name and address it carries.

Every key is read and opened before anything is sent, and one that fails stops the import. A locked key asks for its passphrase, and one passphrase serves the whole run.

Asks for your password even when you are signed in. With no terminal to ask, pass --password-file and --passphrase-file. Only one of them, or SRC, can be -.

```
proton account keys import REF SRC...
```

```bash
proton account keys import alice@proton.me ~/keys/alice-2019.asc
proton account keys import alice@proton.me ~/keys/work.asc ~/keys/old.asc --passphrase-file /run/secrets/key
cat ~/keys/alice-2019.asc | proton account keys import alice@proton.me - --passphrase-file /run/secrets/key
```

| Flag | Description |
| --- | --- |
| `--passphrase-file string` | Read the passphrase that locks the file from a file, or - for stdin |
| `--password-file string` | Read the account password from a file, or - for stdin |
| `--totp string` | Two-factor code |

## `list`

List the keys of the account and its addresses.

STATUS is one of primary, active, obsolete, compromised, locked, forwarding, disabled and unreadable. A locked key comes back with `proton account keys reactivate`.

```
proton account keys list
```

```bash
proton account keys list
proton account keys list --output json
```

## `reactivate`

Bring back the keys a password reset locked.

Give one secret from before the reset - the password, the recovery phrase or a recovery file - and your current password to confirm. With no flag, the previous password is asked for; in two-password mode that is the second one. Keys the secret does not open stay locked and are named. Drive files come back with `proton drive volumes restore`.

```
proton account keys reactivate
```

```bash
proton account keys reactivate
proton account keys reactivate --recovery-phrase
proton account keys reactivate --recovery-file ~/Downloads/proton_recovery.asc
proton account keys reactivate --previous-password-file /run/secrets/proton-old --password-file /run/secrets/proton
```

| Flag | Description |
| --- | --- |
| `--password-file string` | Read the account password from a file, or - for stdin |
| `--previous-password-file string` | Read the password from before the reset from a file, or - for stdin |
| `--recovery-file string` | Recover with a recovery file downloaded from Proton |
| `--recovery-phrase` | Recover with the recovery phrase, asked for at the prompt |
| `--recovery-phrase-file string` | Read the recovery phrase from a file, or - for stdin |
| `--totp string` | Two-factor code |

## `update`

Make a key primary, or mark it obsolete or compromised.

--primary makes it the key its address encrypts to and signs with, and the one it replaces stays. --obsolete stops encryption to the key and keeps its signatures trusted. --compromised stops both, and the key goes on opening what was sealed to it.

=false takes a mark off. --compromised=false leaves the key obsolete, so pass --obsolete=false as well to use it again. The primary key cannot be marked, and only address keys change.

Asks for your password even when you are signed in. With no terminal to ask, pass --password-file, which takes - for stdin.

```
proton account keys update REF
```

```bash
proton account keys update 7Hn2Lw0R --primary
proton account keys update 9d02f7c1 --compromised
proton account keys update 9d02f7c1 --compromised=false --obsolete=false
```

| Flag | Description |
| --- | --- |
| `--compromised` | Stop encrypting to it and trusting its signatures; --compromised=false trusts them again |
| `--obsolete` | Stop encrypting to it; --obsolete=false encrypts to it again |
| `--password-file string` | Read the account password from a file, or - for stdin |
| `--primary` | Make it the key its address encrypts to and signs with |
| `--totp string` | Two-factor code |

---

Every command also takes the [flags that work everywhere](../about/commands.md#flags-that-work-on-every-command).
