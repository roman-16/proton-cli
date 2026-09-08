# proton drive shared

Files and folders other people have shared with you.

Every command under `proton drive shared`, with the arguments and flags it takes. For these commands in use, see [the drive guide](README.md).

Holds `list`.

## `list`

List what other people have shared with you.

These are not in your tree and have no path of their own. To open one, pass `--shared REF` to any `items` command: / is then the item itself, and anything below it is a path inside it.

An item whose name cannot be decrypted is still listed, so you can still act on it by ID.

```
proton drive shared list
```

```bash
proton drive shared list
```

---

Every command also takes the [flags that work everywhere](../about/commands.md#flags-that-work-on-every-command).
