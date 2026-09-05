# proton calendar events

Events in your calendars.

Every command under `proton calendar events`, with the arguments and flags it takes. For these commands in use, see [the calendar guide](README.md).

Holds `create`, `delete`, `export`, `get`, `import`, `list`, `respond` and `update`.

## `create`

Create an event.

--start takes a day and a time, as 2026-04-16T14:00. A bare day is an event with no time of day, which --all-day has to say as well.

Without --end or --duration an event lasts as long as the calendar it is made in says a new event lasts, which `settings calendars get` shows; an all-day event lasts a day.

```
proton calendar events create
```

```bash
proton calendar events create --title Dentist --start 2026-04-16T14:00 --duration 1h
proton calendar events create --title Standup --start 2026-04-16T09:00 --duration 15m --rrule 'FREQ=WEEKLY;COUNT=10' --remind 15m
proton calendar events create --title Holiday --start 2026-07-01 --all-day --calendar Personal
proton calendar events create --title 'Design review' --start 2026-04-20T10:00 --end 2026-04-20T10:45 --attendee jane@example.com --location 'Room 3'
proton calendar events create --title Renewal --start 2026-09-01T09:00 --remind 1d:email
```

| Flag | Description |
| --- | --- |
| `--all-day` | An event with no time of day |
| `--attendee stringArray` | Invite someone, as EMAIL or EMAIL:optional; Proton users are added directly, others are emailed (repeatable) |
| `--calendar string` | Which calendar, by name or ID (default: your first) |
| `--description string` | Set the description |
| `--duration string` | Set how long it lasts (e.g. 15m, 1h, 2h30m, 3d) |
| `--end string` | Set the end: the last day it runs through, or a day and a time |
| `--location string` | Set where it is |
| `--remind stringArray` | Remind this long before the start, as DURATION or DURATION:email (repeatable) |
| `--rrule string` | Set the recurrence rule, e.g. FREQ=WEEKLY;COUNT=10 |
| `--start string` | Set the start: a day, or a day and a time (2026-04-16, 2026-04-16T14:00, or full RFC 3339) |
| `--status string` | Set whether it is going ahead: confirmed, tentative, cancelled |
| `--title string` | Set the title |

## `delete`

Delete events.

A reference that names one occurrence of a recurring event deletes only that occurrence. Add --onwards to delete it and every later one, or drop the @ part of the reference to delete the whole series.

```
proton calendar events delete REF...
```

```bash
proton calendar events delete Dentist
proton calendar events delete 4f2a1b9c@2026-05-04T09:00 --onwards
```

| Flag | Description |
| --- | --- |
| `--onwards` | Also delete every later occurrence of the series |

## `export`

Write events out as an .ics file.

--start and --end are whole days in your own zone, both included.

A recurring series is written once, with its rule, so another client reads it back as the same series.

```
proton calendar events export
```

```bash
proton calendar events export --start 2026-01-01 --end 2026-12-31 --dest year.ics
proton calendar events export --calendar Work --dest - > work.ics
```

| Flag | Description |
| --- | --- |
| `--calendar string` | Which calendar, by name or ID (default: all of them) |
| `--dest string` | Write to this path, or - for stdout |
| `--dest-dir string` | Write into this directory, keeping each item's own name |
| `--end string` | Last day to include (YYYY-MM-DD) |
| `--force` | Overwrite a file that already exists |
| `--start string` | First day to include (YYYY-MM-DD) |

## `get`

Show one event, decrypted.

```
proton calendar events get REF
```

```bash
proton calendar events get Dentist
proton calendar events get 4f2a1b9c@2026-04-22T09:00
```

## `import`

Read events in from an .ics file, or from stdin with -.

An event is addressed by its UID, so reading a file back changes that event rather than making a second one.

Participants are left out. An imported event is a record; no invitations are sent.

```
proton calendar events import PATH
```

```bash
proton calendar events import holidays.ics
proton calendar events import --calendar Work team.ics
curl -s https://example.com/team.ics | proton calendar events import -
```

| Flag | Description |
| --- | --- |
| `--calendar string` | Which calendar to import into, by name or ID (default: your first) |

## `list`

List what is on your calendars between two dates.

--start and --end are whole days in your own zone, both included.

Each occurrence of a recurring event is listed on its own day, with a reference that names that occurrence.

Covers every calendar unless --calendar narrows it to one.

```
proton calendar events list
```

```bash
proton calendar events list
proton calendar events list --start 2026-04-15 --end 2026-04-30
proton calendar events list --calendar Work
```

| Flag | Description |
| --- | --- |
| `--calendar string` | Which calendar, by name or ID (default: all of them) |
| `--end string` | Last day to include (YYYY-MM-DD) |
| `--start string` | First day to include (YYYY-MM-DD) |

## `respond`

Answer an invitation, telling the organizer.

```
proton calendar events respond REF
```

```bash
proton calendar events respond 'Team sync' --answer accept
proton calendar events respond 'Team sync' --answer decline
```

| Flag | Description |
| --- | --- |
| `--answer string` | Your reply to the invitation: accept, tentative, decline |

## `update`

Change an event.

Anything you do not mention is left alone, including the reminders and the recurrence.

--start takes a day, which moves the event and leaves its time of day alone, or a day and a time, which gives an all-day event a time of day and makes it last what the calendar says a new event lasts. --all-day takes the time of day away again; --all-day=false is the other direction and needs a --start saying which time.

A reference that names one occurrence of a recurring event changes only that occurrence. Add --onwards to change it and every later one, or drop the @ part of the reference to change the whole series, which --dry-run will show you before you do.

```
proton calendar events update REF
```

```bash
proton calendar events update Dentist --start 2026-04-16T15:30
proton calendar events update 4f2a1b9c@2026-04-22T09:00 --location 'Room 3'
proton calendar events update 4f2a1b9c@2026-04-22T09:00 --title Standup --onwards
proton calendar events update Offsite --all-day
proton calendar events update Offsite --all-day=false --start 2026-07-01T09:00 --end 2026-07-01T17:00
```

| Flag | Description |
| --- | --- |
| `--all-day` | Turn it into an event with no time of day |
| `--description string` | Replace the description |
| `--duration string` | Replace how long it lasts (e.g. 15m, 1h, 2h30m, 3d) |
| `--end string` | Replace the end: the last day it runs through, or a day and a time |
| `--location string` | Replace where it is |
| `--no-remind` | Remove the reminders |
| `--onwards` | Also change every later occurrence of the series |
| `--remind stringArray` | Remind this long before the start, as DURATION or DURATION:email (repeatable) |
| `--rrule string` | Replace the recurrence rule, e.g. FREQ=WEEKLY;COUNT=10 |
| `--start string` | Replace the start: a day, or a day and a time (2026-04-16, 2026-04-16T14:00, or full RFC 3339) |
| `--status string` | Replace whether it is going ahead: confirmed, tentative, cancelled |
| `--title string` | Replace the title |

---

Every command also takes the [flags that work everywhere](../about/commands.md#flags-that-work-on-every-command).
