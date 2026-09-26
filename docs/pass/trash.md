# proton pass trash

Items you have removed but not yet deleted.

Every command under `proton pass trash`, with the arguments and flags it takes. For these commands in use, see [the pass guide](README.md).

Holds `empty`, `list` and `restore`.

## `empty`

Delete everything in the trash, permanently.

What a hidden vault holds in its trash is left alone. `vaults unhide` brings it back into reach.

```
proton pass trash empty
```

```bash
proton pass trash empty
```

## `list`

List what is in the trash.

What a hidden vault holds in its trash is left out.

```
proton pass trash list
```

```bash
proton pass trash list
```

| Flag | Description |
| --- | --- |
| `--desc` | Reverse the order |
| `--limit int` | How many items per page; 0 for all of them |
| `--page int` | Which page of results, counting from zero |
| `--sort string` | Order by: name, type, modified (default `name`) |

## `restore`

Put items back where they came from.

```
proton pass trash restore [REF...]
```

```bash
proton pass trash restore GitHub
proton pass trash restore --all
```

| Flag | Description |
| --- | --- |
| `--all` | Act on everything in scope, rather than a subset |

---

Every command also takes the [flags that work everywhere](../about/commands.md#flags-that-work-on-every-command).
