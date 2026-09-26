# proton pass vaults

The vaults your items live in.

Every command under `proton pass vaults`, with the arguments and flags it takes. For these commands in use, see [the pass guide](README.md).

Holds `create`, `delete`, `get`, `hide`, `list`, `share`, `transfer`, `unhide` and `update`.

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

## `hide`

Keep vaults out of your listings.

A hidden vault is left out of `items list`, the trash, `aliases list`, `sharing list`, the password checks and `breaches list`, and looking an item up by name does not search it. Naming it still reaches it, as in `items list --vault Archive`, and so does an item's ID. `vaults list` shows every vault, hidden or not.

Only you stop seeing it: the other members of a shared vault are not affected.

```
proton pass vaults hide REF...
```

```bash
proton pass vaults hide Archive
proton pass vaults hide Archive 'Old work'
```

## `list`

List your vaults.

```
proton pass vaults list
```

```bash
proton pass vaults list
```

| Flag | Description |
| --- | --- |
| `--desc` | Reverse the order |
| `--limit int` | How many vaults per page; 0 for all of them |
| `--page int` | Which page of results, counting from zero |
| `--sort string` | Order by: name, members (default `name`) |

## `share`

Who else can open a vault.

Holds `add`, `confirm`, `get`, `remove` and `update`.

### `share add`

Offer a vault to somebody.

They are sent an invitation and see nothing until they take it.

EMAIL may be an address outside Proton. Proton emails them an invitation to create an account, and nothing reaches them until they have one and you run `share confirm`.

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

### `share confirm`

Let somebody into a vault once they join Proton.

Use it for an address that had no Proton account when you offered it. It is refused until the account exists, and `share get` says who is ready. Afterwards they hold an ordinary invitation, which they still have to accept.

```
proton pass vaults share confirm REF EMAIL
```

```bash
proton pass vaults share confirm Work jane@example.com
```

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

## `unhide`

Bring hidden vaults back into your listings.

```
proton pass vaults unhide REF...
```

```bash
proton pass vaults unhide Archive
```

## `update`

Rename a vault, or change how it looks.

Icons and colors are named: --icon star, --color teal.

Anything you do not mention is left alone, including a description written in the Pass app.

```
proton pass vaults update REF
```

```bash
proton pass vaults update Work --name Office
proton pass vaults update Work --description 'Shared team logins' --icon star --color teal
```

| Flag | Description |
| --- | --- |
| `--color string` | Which of Pass's vault colors it takes: violet, pink, yellow, green, blue, magenta, red, orange, grey, teal |
| `--description string` | What the vault is for |
| `--icon string` | Which of Pass's icons represents it: home, work, gift, shop, heart, bear, circles, flower, group, pacman, shopping-cart, leaf, shield, basketball, credit-card, fish, smile, lock, mushroom, star, fire, wallet, bookmark, cream, laptop, json, book, box, atom, cheque |
| `--name string` | New name |

---

Every command also takes the [flags that work everywhere](../about/commands.md#flags-that-work-on-every-command).
