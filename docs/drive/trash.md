# proton drive trash

Items you have removed but not yet deleted.

Every command under `proton drive trash`, with the arguments and flags it takes. For these commands in use, see [the drive guide](README.md).

Holds `empty`, `list` and `restore`.

## `empty`

Delete everything in the trash, permanently.

That is everything `trash list` shows, trashed photos included.

```
proton drive trash empty
```

```bash
proton drive trash empty
```

## `list`

List what is in the trash.

This covers everything the account has trashed, photos included. `trash empty` deletes all of it.

A trashed item has no path, so address it by the ID shown here. An item whose name cannot be decrypted is still listed, so you can still act on it.

```
proton drive trash list
```

```bash
proton drive trash list
proton drive trash list --sort trashed --desc
```

| Flag | Description |
| --- | --- |
| `--desc` | Reverse the order |
| `--page int` | Which page of results, counting from zero |
| `--page-size int` | How many items per page; 0 for all of them (default `50`) |
| `--sort string` | Order by: name, size, trashed (default `name`) |

## `restore`

Put items back where they came from.

A trashed item has no path. Name it by the ID that `trash list` shows.

```
proton drive trash restore REF...
```

```bash
proton drive trash restore 5bH2mQxK
```

---

Every command also takes the [flags that work everywhere](../about/commands.md#flags-that-work-on-every-command).
