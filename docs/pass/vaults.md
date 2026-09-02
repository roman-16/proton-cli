# proton pass vaults

The vaults your items live in.

Every command under `proton pass vaults`, with the arguments and flags it takes. For these commands in use, see [the pass guide](README.md).

Holds `create`, `delete`, `get`, `list`, `share`, `transfer` and `update`.

## `create`

Create a vault.

```
proton pass vaults create
```

```bash
proton pass vaults create --name Work
```

| Flag | Description |
| --- | --- |
| `--name string` | Name for the new vault |

## `delete`

Delete vaults, and everything in them.

```
proton pass vaults delete REF...
```

```bash
proton pass vaults delete Work
```

## `get`

Show one vault in full.

```
proton pass vaults get REF
```

```bash
proton pass vaults get Work
```

## `list`

List your vaults.

```
proton pass vaults list
```

```bash
proton pass vaults list
```

## `share`

Who else can open a vault.

Holds `add`, `get`, `remove` and `update`.

### `share add`

Offer a vault to somebody.

They are sent an invitation and see nothing until they take it. What is sent is the key that opens the vault, encrypted to their key and signed with yours - so it has to be another Proton account, because an address Proton holds no keys for has nothing to encrypt to.

```
proton pass vaults share add REF EMAIL
```

```bash
proton pass vaults share add Work jane@proton.me
proton pass vaults share add Work jane@proton.me --access editor
```

| Flag | Description |
| --- | --- |
| `--access string` | What they may do with it: viewer, editor, manager (default `viewer`) |

### `share get`

Show who can open a vault.

Members have accepted; the invited have not answered yet.

```
proton pass vaults share get REF
```

```bash
proton pass vaults share get Work
```

### `share remove`

Take somebody's access to a vault away.

It withdraws an invitation nobody answered, or removes a member who did. The vault is untouched; anything they already read they have read.

```
proton pass vaults share remove REF EMAIL
```

```bash
proton pass vaults share remove Work jane@proton.me
```

### `share update`

Change what somebody may do with a vault.

For a member, nothing is re-encrypted: the key they hold still opens the vault, and only what they may do with it changes. Somebody who has not answered yet holds nothing to change, so the offer is withdrawn and made again at the new access - which sends them a fresh invitation.

```
proton pass vaults share update REF EMAIL
```

```bash
proton pass vaults share update Work jane@proton.me --access manager
```

| Flag | Description |
| --- | --- |
| `--access string` | What they may do with it: viewer, editor, manager (default `viewer`) |

## `transfer`

Make somebody else the owner of a vault.

They have to be a member already, and only the owner can hand a vault over.

Afterwards you are a manager like anybody else. This is the one change to a vault you cannot undo on your own.

```
proton pass vaults transfer REF EMAIL
```

```bash
proton pass vaults transfer Work jane@proton.me
```

## `update`

Rename a vault, or change how it looks.

Icons and colors are numbers, because Pass shows them as an unnamed grid: --icon 7, --color 3.

Anything you do not mention is left alone, including a description written in the Pass app.

```
proton pass vaults update REF
```

```bash
proton pass vaults update Work --name Office
proton pass vaults update Work --description 'Shared team logins' --icon 7 --color 3
```

| Flag | Description |
| --- | --- |
| `--color string` | Which of Pass's vault colors it takes: 1, 2, 3, 4, 5, 6, 7, 8, 9, 10 |
| `--description string` | What the vault is for |
| `--icon string` | Which of Pass's icons represents it: 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30 |
| `--name string` | New name |

---

Every command also takes the [flags that work everywhere](../about/commands.md#flags-that-work-on-every-command).
