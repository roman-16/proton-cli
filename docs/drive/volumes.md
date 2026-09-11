# proton drive volumes

The volumes your files and photos are kept on.

Every command under `proton drive volumes`, with the arguments and flags it takes. For these commands in use, see [the drive guide](README.md).

Holds `delete`, `list` and `restore`.

## `delete`

Delete a locked volume and everything on it.

Only a locked volume can be deleted. Proton removes its files within 72 hours and nothing brings them back afterwards.

```
proton drive volumes delete [REF...]
```

```bash
proton drive volumes delete 7Kd91mQx
proton drive volumes delete 7Kd91mQx --yes
```

| Flag | Description |
| --- | --- |
| `--all` | Act on everything in scope, rather than a subset |

## `list`

List the volumes your files and photos are kept on.

There is one for files and, on newer accounts, one for photos. A password reset locks a volume and a fresh one takes its place, so a locked volume holds what you had before the reset. RESTORE says how far `volumes restore` has got with one.

```
proton drive volumes list
```

```bash
proton drive volumes list
```

## `restore`

Put the files of a locked volume back.

The keys the volume was locked with have to be active first: reactivate them with `proton account keys reactivate`. Files come back as a folder named `Restored files` and the date; computers and the photo library stay where they are. Proton moves the files in its own time, so they appear as it finishes.

A locked photo volume is skipped until a Proton client has made a photo library for it to come back into.

```
proton drive volumes restore [REF...]
```

```bash
proton drive volumes restore 7Kd91mQx
proton drive volumes restore --all
```

| Flag | Description |
| --- | --- |
| `--all` | Act on everything in scope, rather than a subset |

---

Every command also takes the [flags that work everywhere](../about/commands.md#flags-that-work-on-every-command).
