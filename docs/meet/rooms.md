# proton meet rooms

Rooms that stay open, your personal room first.

Every command under `proton meet rooms`, with the arguments and flags it takes. For these commands in use, see [the meet guide](README.md).

Holds `create`, `delete`, `get`, `list`, `rotate` and `update`.

## `create`

Create a room and print its link. A room has no time: its link works until you delete it.

```
proton meet rooms create
```

```bash
proton meet rooms create --title 'Team room'
```

| Flag | Description |
| --- | --- |
| `--title string` | Set the title |

## `delete`

Delete a room. Its link stops working for everyone who has it.

REF is the ID its link shows after id-, such as E87W2DAJ60, the whole link, its title, or personal for your personal room.

```
proton meet rooms delete REF
```

```bash
proton meet rooms delete 'Platform room'
```

## `get`

Show one room, link and all.

REF is the ID its link shows after id-, such as E87W2DAJ60, the whole link, its title, or personal for your personal room.

```
proton meet rooms get REF
```

```bash
proton meet rooms get personal
proton meet rooms get 'Team room'
```

## `list`

List your rooms, your personal room first.

The links are not shown: anyone holding one can join. `rooms get` shows a link.

```
proton meet rooms list
```

```bash
proton meet rooms list
```

| Flag | Description |
| --- | --- |
| `--desc` | Reverse the order |
| `--limit int` | How many rooms per page; 0 for all of them |
| `--page int` | Which page of results, counting from zero |
| `--sort string` | Order by: title (default `title`) |

## `rotate`

Give your personal room a new link, and print it. The old link stops working for everyone who has it. It can change once every 24 hours.

REF is personal, the one room whose link can change.

```
proton meet rooms rotate REF
```

```bash
proton meet rooms rotate personal
```

## `update`

Rename a room. Its link stays as it is.

REF is the ID its link shows after id-, such as E87W2DAJ60, the whole link, its title, or personal for your personal room.

```
proton meet rooms update REF
```

```bash
proton meet rooms update 'Team room' --title 'Platform room'
proton meet rooms update personal --title 'Office hours'
```

| Flag | Description |
| --- | --- |
| `--title string` | Replace the title |

---

Every command also takes the [flags that work everywhere](../about/commands.md#flags-that-work-on-every-command).
