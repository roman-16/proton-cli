# proton contacts groups

Contact groups.

Every command under `proton contacts groups`, with the arguments and flags it takes. For these commands in use, see [the contacts guide](README.md).

Holds `add`, `create`, `delete`, `get`, `list`, `remove` and `update`.

## `add`

Add contacts to a group.

Proton groups addresses rather than people, so a colleague's work address can be in a group while their personal one is not. Naming a contact means all of their addresses; --email narrows it to the ones you name, and then exactly one contact may be named.

```
proton contacts groups add REF CONTACT_REF...
```

```bash
proton contacts groups add Team jane
```

| Flag | Description |
| --- | --- |
| `--email stringArray` | Act on this address only, rather than all of the contact's (repeatable) |

## `create`

Create a contact group.

```
proton contacts groups create
```

```bash
proton contacts groups create --name Team
proton contacts groups create --name Family --color strawberry
```

| Flag | Description |
| --- | --- |
| `--color string` | Accent color, by name (purple) or hex (#8080FF) (default `#8080FF`) |
| `--name string` | Group name |

## `delete`

Delete contact groups.

```
proton contacts groups delete REF...
```

```bash
proton contacts groups delete Team
```

## `get`

Show one group and the addresses in it.

```
proton contacts groups get REF
```

```bash
proton contacts groups get Team
```

## `list`

List contact groups.

```
proton contacts groups list
```

```bash
proton contacts groups list
```

## `remove`

Remove contacts from a group.

Proton groups addresses rather than people, so a colleague's work address can be in a group while their personal one is not. Naming a contact means all of their addresses; --email narrows it to the ones you name, and then exactly one contact may be named.

```
proton contacts groups remove REF CONTACT_REF...
```

```bash
proton contacts groups remove Team jane
```

| Flag | Description |
| --- | --- |
| `--email stringArray` | Act on this address only, rather than all of the contact's (repeatable) |

## `update`

Rename or recolor a contact group.

```
proton contacts groups update REF
```

```bash
proton contacts groups update Team --name Engineering
proton contacts groups update Team --color reef
```

| Flag | Description |
| --- | --- |
| `--color string` | New accent color, as a hex value |
| `--name string` | New group name |

---

Every command also takes the [flags that work everywhere](../about/commands.md#flags-that-work-on-every-command).
