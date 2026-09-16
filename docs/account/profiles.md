# proton account profiles

Accounts signed in on this machine.

Every command under `proton account profiles`, with the arguments and flags it takes. For these commands in use, see [the account guide](README.md).

Holds `delete` and `list`.

## `delete`

Remove a profile and everything it keeps on this machine.

```
proton account profiles delete REF...
```

```bash
proton account profiles delete work
```

## `list`

List the profiles with a saved session.

```
proton account profiles list
```

```bash
proton account profiles list
```

| Flag | Description |
| --- | --- |
| `--desc` | Reverse the order |
| `--limit int` | How many profiles per page; 0 for all of them |
| `--page int` | Which page of results, counting from zero |
| `--sort string` | Order by: name, saved (default `name`) |

---

Every command also takes the [flags that work everywhere](../about/commands.md#flags-that-work-on-every-command).
