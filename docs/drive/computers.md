# proton drive computers

Computers syncing files to Drive.

Every command under `proton drive computers`, with the arguments and flags it takes. For these commands in use, see [the drive guide](README.md).

Holds `delete`, `list` and `update`.

## `delete`

Remove computers from Drive.

The computer stops syncing and leaves the list. Set it up again by signing the desktop app in on it once more.

```
proton drive computers delete REF...
```

```bash
proton drive computers delete 7Kd91mQx
```

## `list`

List the computers syncing to Drive.

A computer appears here once the Proton Drive desktop app is signed in on it; nothing else puts one on the list. SYNCED is when it last synced, and a dash means it never has.

What a computer syncs is a tree of its own. Pass `--computer REF` to any `items` command to work inside it.

```
proton drive computers list
```

```bash
proton drive computers list
```

## `update`

Rename a computer.

The name is the computer's everywhere: the desktop app and the web client show what you set here. The files it syncs are untouched.

```
proton drive computers update REF
```

```bash
proton drive computers update 'Work laptop' --name 'Office PC'
```

| Flag | Description |
| --- | --- |
| `--name string` | New name for the computer |

---

Every command also takes the [flags that work everywhere](../about/commands.md#flags-that-work-on-every-command).
