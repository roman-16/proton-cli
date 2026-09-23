# proton calendar invitations

Calendars other people have offered you.

Every command under `proton calendar invitations`, with the arguments and flags it takes. For these commands in use, see [the calendar guide](README.md).

Holds `accept`, `decline` and `list`.

## `accept`

Take a calendar somebody offered you.

It then lists as shared. With editor access you can change what is on it; with viewer access you can only read it. `settings calendars leave` gives it up again.

```
proton calendar invitations accept REF...
```

```bash
proton calendar invitations accept Work
```

## `decline`

Turn down a calendar somebody offered you.

```
proton calendar invitations decline REF...
```

```bash
proton calendar invitations decline Work
```

## `list`

List calendars other people have offered you.

```
proton calendar invitations list
```

```bash
proton calendar invitations list
```

| Flag | Description |
| --- | --- |
| `--desc` | Reverse the order |
| `--limit int` | How many invitations per page; 0 for all of them |
| `--page int` | Which page of results, counting from zero |
| `--sort string` | Order by: name, sender (default `name`) |

---

Every command also takes the [flags that work everywhere](../about/commands.md#flags-that-work-on-every-command).
