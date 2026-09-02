# proton calendar invitations

Calendars other people have offered you.

Every command under `proton calendar invitations`, with the arguments and flags it takes. For these commands in use, see [the calendar guide](README.md).

Holds `accept`, `decline` and `list`.

## `accept`

Take a calendar somebody offered you.

The invitation carries the calendar's key, encrypted to the address it was sent to. Accepting re-encrypts that key to your own, after which the calendar behaves like any other of yours.

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

---

Every command also takes the [flags that work everywhere](../about/commands.md#flags-that-work-on-every-command).
