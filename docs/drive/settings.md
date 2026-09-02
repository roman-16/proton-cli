# proton drive settings

How Drive behaves.

Every command under `proton drive settings`, with the arguments and flags it takes. For these commands in use, see [the drive guide](README.md).

Holds `get`, `list` and `set`.

## `get`

Show the drive settings now in effect.

```
proton drive settings get
```

```bash
proton drive settings get
```

## `list`

List the drive settings that can be changed.

```
proton drive settings list
```

```bash
proton drive settings list
```

## `set`

Change one drive setting.

```
proton drive settings set KEY VALUE
```

```bash
proton drive settings set revision-retention 30
```

---

Every command also takes the [flags that work everywhere](../about/commands.md#flags-that-work-on-every-command).
