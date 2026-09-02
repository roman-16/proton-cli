# proton calendar settings

How Calendar behaves.

Every command under `proton calendar settings`, with the arguments and flags it takes. For these commands in use, see [the calendar guide](README.md).

Holds `calendars`, `get`, `list` and `set`.

## `calendars`

The calendars you keep events in.

Holds `create`, `delete`, `get`, `list`, `share` and `update`.

### `calendars create`

Create a calendar, or subscribe to one published elsewhere.

--url takes the address of an .ics file. Proton fetches it on a schedule and fills the calendar from it, so those events are read-only. An address Proton cannot read is refused before the calendar is made.

```
proton calendar settings calendars create
```

```bash
proton calendar settings calendars create --name Work
proton calendar settings calendars create --name Personal --color pacific
proton calendar settings calendars create --name Timetable --url https://example.com/team.ics
```

| Flag | Description |
| --- | --- |
| `--color string` | Accent color, by name (purple) or hex (#8080FF) (default `#8080FF`) |
| `--name string` | Name for the new calendar |
| `--url string` | Subscribe to the calendar published at this address instead of making an empty one |

### `calendars delete`

Delete calendars, and every event in them.

Proton guards this behind an elevated session, so it asks for your password even when a saved session already exists. With no terminal to ask, pass --password-file or --password-stdin.

```
proton calendar settings calendars delete REF...
```

```bash
proton calendar settings calendars delete Work
```

| Flag | Description |
| --- | --- |
| `--password-file string` | Read the account password from a file |
| `--password-stdin` | Read the account password from stdin |
| `--totp string` | Two-factor code |

### `calendars get`

Show one calendar, with the defaults it gives new events.

```
proton calendar settings calendars get REF
```

```bash
proton calendar settings calendars get Work
```

### `calendars list`

List your calendars.

```
proton calendar settings calendars list
```

```bash
proton calendar settings calendars list
```

### `calendars share`

Who else can see a calendar.

Holds `add`, `list` and `remove`.

### `calendars share add`

Give somebody a calendar.

It has to be another Proton account: what travels is the key that opens the calendar, encrypted to their key.

They are sent an invitation and see nothing until they accept. They can then read the calendar; --edit lets them change it too.

```
proton calendar settings calendars share add REF EMAIL
```

```bash
proton calendar settings calendars share add Work jane@proton.me
proton calendar settings calendars share add Work jane@proton.me --edit
```

| Flag | Description |
| --- | --- |
| `--edit` | Let them change the calendar, not just see it |

### `calendars share list`

List who has a calendar.

Somebody who has not answered yet is listed as pending. They can see nothing until they accept.

```
proton calendar settings calendars share list REF
```

```bash
proton calendar settings calendars share list Work
```

### `calendars share remove`

Take somebody's access to a calendar away.

Works whether they accepted or not. An unanswered invitation is withdrawn; an accepted membership is ended.

```
proton calendar settings calendars share remove REF EMAIL
```

```bash
proton calendar settings calendars share remove Work jane@proton.me
```

### `calendars update`

Rename or recolor a calendar, or change what it gives new events.

Defaults are set per calendar, so a work calendar can open half-hour meetings with a reminder while a personal one does not.

```
proton calendar settings calendars update REF
```

```bash
proton calendar settings calendars update Work --name Office
proton calendar settings calendars update Work --color enzian
proton calendar settings calendars update Work --default-duration 30m --remind 15m
proton calendar settings calendars update Personal --busy off
```

| Flag | Description |
| --- | --- |
| `--busy string` | Whether events here make you look busy to others: on, off |
| `--color string` | New accent color, as a hex value |
| `--default-duration string` | How long a new event lasts unless it says otherwise (e.g. 30m, 1h) |
| `--name string` | New name |
| `--no-remind` | Give new events no reminder by default |
| `--remind stringArray` | Default reminder for a new event, as DURATION or DURATION:email (repeatable) |
| `--remind-all-day stringArray` | Default reminder for a new all-day event (repeatable) |

## `get`

Show the calendar settings now in effect.

```
proton calendar settings get
```

```bash
proton calendar settings get
```

## `list`

List the calendar settings that can be changed.

```
proton calendar settings list
```

```bash
proton calendar settings list
```

## `set`

Change one calendar setting.

```
proton calendar settings set KEY VALUE
```

```bash
proton calendar settings set week-start monday
proton calendar settings set default-duration 30
```

---

Every command also takes the [flags that work everywhere](../about/commands.md#flags-that-work-on-every-command).
