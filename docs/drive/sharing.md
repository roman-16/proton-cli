# proton drive sharing

What you have shared with other people.

Every command under `proton drive sharing`, with the arguments and flags it takes. For these commands in use, see [the drive guide](README.md).

Holds `list`.

## `list`

List the files and folders you have handed to named people.

A public link is the other way to share, and `links list` has those. To check a single item instead, run `items share get PATH`.

```
proton drive sharing list
```

```bash
proton drive sharing list
```

| Flag | Description |
| --- | --- |
| `--desc` | Reverse the order |
| `--limit int` | How many items per page; 0 for all of them |
| `--page int` | Which page of results, counting from zero |
| `--sort string` | Order by: name, size, shared (default `name`) |

---

Every command also takes the [flags that work everywhere](../about/commands.md#flags-that-work-on-every-command).
