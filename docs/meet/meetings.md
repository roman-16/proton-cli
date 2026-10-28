# proton meet meetings

Meetings at a set time, once or repeating.

Every command under `proton meet meetings`, with the arguments and flags it takes. For these commands in use, see [the meet guide](README.md).

Holds `create`, `delete`, `get`, `list` and `update`.

## `create`

Schedule a meeting and print its link.

--start takes a day and a time, as 2026-11-04T09:00, read in your zone. Without --end or --duration a meeting lasts 30 minutes, and --rrule makes it repeat.

A meeting made here is in Proton Meet alone. `calendar events create --meet` makes one on an event, and invites the people on it.

```
proton meet meetings create
```

```bash
proton meet meetings create --title 'Weekly sync' --start 2026-11-04T09:00 --rrule FREQ=WEEKLY
proton meet meetings create --title 'Interview' --start 2026-11-05T14:00 --duration 1h
```

| Flag | Description |
| --- | --- |
| `--duration string` | Set how long it lasts (e.g. 15m, 1h, 1h30m) |
| `--end string` | Set the end: a day and a time |
| `--rrule string` | Set the recurrence rule, e.g. FREQ=WEEKLY;COUNT=10 |
| `--start string` | Set the start: a day and a time (2026-11-04T09:00, or full RFC 3339) |
| `--title string` | Set the title |

## `delete`

Delete a meeting. Its link stops working for everyone who has it.

A meeting that belongs to a calendar event is deleted all the same, and the event keeps a link that no longer works.

```
proton meet meetings delete REF
```

```bash
proton meet meetings delete 'Weekly sync'
```

## `get`

Show one meeting, link and all.

REF is the ID its link shows after id-, such as 6MCF0BSHBR, the whole link, or its title. A meeting made for a calendar event opens by its whole link.

```
proton meet meetings get REF
```

```bash
proton meet meetings get 'Weekly sync'
proton meet meetings get https://meet.proton.me/join/id-865JZ8JMHG
```

## `list`

List your meetings, the next one first.

A meeting that repeats is listed at its next occurrence. The links are not shown: anyone holding one can join. `meetings get` shows a link.

```
proton meet meetings list
```

```bash
proton meet meetings list
proton meet meetings list --sort title --output json
```

| Flag | Description |
| --- | --- |
| `--desc` | Reverse the order |
| `--limit int` | How many meetings per page; 0 for all of them |
| `--page int` | Which page of results, counting from zero |
| `--sort string` | Order by: next, title (default `next`) |

## `update`

Rename a meeting, or move it to another time.

Anything you do not mention is left alone. A meeting you move keeps its length unless --end or --duration says otherwise, and --rrule '' makes it happen once.

A meeting that belongs to a calendar event changes with the event, through `calendar events update`.

```
proton meet meetings update REF
```

```bash
proton meet meetings update 'Weekly sync' --start 2026-11-04T10:00
proton meet meetings update 865JZ8JM --title 'Team sync' --rrule ''
```

| Flag | Description |
| --- | --- |
| `--duration string` | Replace how long it lasts (e.g. 15m, 1h, 1h30m) |
| `--end string` | Replace the end: a day and a time |
| `--rrule string` | Replace the recurrence rule, e.g. FREQ=WEEKLY;COUNT=10 |
| `--start string` | Replace the start: a day and a time (2026-11-04T09:00, or full RFC 3339) |
| `--title string` | Replace the title |

---

Every command also takes the [flags that work everywhere](../about/commands.md#flags-that-work-on-every-command).
