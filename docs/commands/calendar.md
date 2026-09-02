# proton calendar

Calendars and events.

Every command under `proton calendar`, with the arguments and flags it takes. For these commands in use, see [the guide](../apps/calendar.md).

Holds `events`, `invitations`, `reminders` and `settings`.

## `events`

Events in your calendars.

Holds `create`, `delete`, `export`, `get`, `import`, `list`, `respond` and `update`.

### `events create`

Create an event.

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
| `--end string` | Set the end (RFC 3339, or YYYY-MM-DDTHH:MM) |
| `--location string` | Set where it is |
| `--remind stringArray` | Remind this long before the start, as DURATION or DURATION:email (repeatable) |
| `--rrule string` | Set the recurrence rule, e.g. FREQ=WEEKLY;COUNT=10 |
| `--start string` | Set the start (RFC 3339, or YYYY-MM-DDTHH:MM) |
| `--status string` | Set whether it is going ahead: confirmed, tentative, cancelled |
| `--title string` | Set the title |

### `events delete`

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

### `events export`

Write events out as an .ics file.

--start and --end are whole days in your own zone, both included. A recurring series is written once with its rule, so another client can read it back as the same series.

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

### `events get`

Show one event, decrypted.

```
proton calendar events get REF
```

```bash
proton calendar events get Dentist
proton calendar events get 4f2a1b9c@2026-04-22T09:00
```

### `events import`

Read events in from an .ics file, or from stdin with -.

An event is addressed by its UID, so reading a file back changes that event rather than making a second one.

Participants are left out: an imported event is a record, not an invitation being reissued.

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

### `events list`

List what is on your calendars between two dates.

--start and --end are whole days in your own zone, both included, and nothing outside them is reported. A recurring event is stored once and happens many times, so each occurrence is listed on its own day with a reference that names it. Every calendar is included unless --calendar narrows it to one.

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

### `events respond`

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

### `events update`

Change an event.

Anything you do not mention is left alone, including the reminders and the recurrence.

A reference that names one occurrence of a recurring event changes only that occurrence. Add --onwards to change it and every later one, or drop the @ part of the reference to change the whole series, which --dry-run will show you before you do.

```
proton calendar events update REF
```

```bash
proton calendar events update Dentist --start 2026-04-16T15:30
proton calendar events update 4f2a1b9c@2026-04-22T09:00 --location 'Room 3'
proton calendar events update 4f2a1b9c@2026-04-22T09:00 --title Standup --onwards
```

| Flag | Description |
| --- | --- |
| `--all-day` | Turn it into an event with no time of day |
| `--description string` | Replace the description |
| `--duration string` | Replace how long it lasts (e.g. 15m, 1h, 2h30m, 3d) |
| `--end string` | Replace the end (RFC 3339, or YYYY-MM-DDTHH:MM) |
| `--location string` | Replace where it is |
| `--no-remind` | Remove the reminders |
| `--onwards` | Also change every later occurrence of the series |
| `--remind stringArray` | Remind this long before the start, as DURATION or DURATION:email (repeatable) |
| `--rrule string` | Replace the recurrence rule, e.g. FREQ=WEEKLY;COUNT=10 |
| `--start string` | Replace the start (RFC 3339, or YYYY-MM-DDTHH:MM) |
| `--status string` | Replace whether it is going ahead: confirmed, tentative, cancelled |
| `--title string` | Replace the title |

## `invitations`

Calendars other people have offered you.

Holds `accept`, `decline` and `list`.

### `invitations accept`

Take a calendar somebody offered you.

The invitation carries the calendar's key, encrypted to the address it was sent to. Accepting opens it and signs it back, and the calendar then reads like any other one of yours.

```
proton calendar invitations accept REF...
```

```bash
proton calendar invitations accept Work
```

### `invitations decline`

Turn down a calendar somebody offered you.

```
proton calendar invitations decline REF...
```

```bash
proton calendar invitations decline Work
```

### `invitations list`

List calendars other people have offered you.

```
proton calendar invitations list
```

```bash
proton calendar invitations list
```

## `reminders`

The notifications your events will raise.

Holds `list` and `watch`.

### `reminders list`

List every reminder your events will raise between two dates.

A reminder is listed on the day it goes off, not the day its event is on, so an event with two reminders is two rows and a recurring one is a row per occurrence.

Emailed reminders are Proton's to send and are left out.

```
proton calendar reminders list
```

```bash
proton calendar reminders list
proton calendar reminders list --start 2026-04-20 --end 2026-04-21
proton calendar reminders list --calendar Work --output json
```

| Flag | Description |
| --- | --- |
| `--calendar string` | Which calendar, by name or ID (default: all of them) |
| `--end string` | Last day to include (YYYY-MM-DD) |
| `--start string` | First day to include (YYYY-MM-DD) |

### `reminders watch`

Print each reminder as it comes due, until you stop it.

It sleeps until the moment rather than polling, so a reminder lands on the second, and re-reads your calendars as it goes so an event added meanwhile still reminds you.

Each line says what a notification would say.

```
proton calendar reminders watch
```

```bash
proton calendar reminders watch
proton calendar reminders watch --calendar Work
proton calendar reminders watch --output json
```

| Flag | Description |
| --- | --- |
| `--calendar string` | Which calendar, by name or ID (default: all of them) |

## `settings`

How Calendar behaves.

Holds `calendars`, `get`, `list` and `set`.

### `settings calendars`

The calendars you keep events in.

Holds `create`, `delete`, `get`, `list`, `share` and `update`.

### `settings calendars create`

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

### `settings calendars delete`

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

### `settings calendars get`

Show one calendar, with the defaults it gives new events.

```
proton calendar settings calendars get REF
```

```bash
proton calendar settings calendars get Work
```

### `settings calendars list`

List your calendars.

```
proton calendar settings calendars list
```

```bash
proton calendar settings calendars list
```

### `settings calendars share`

Who else can see a calendar.

Holds `add`, `list` and `remove`.

### `settings calendars share add`

Give somebody a calendar.

They are sent an invitation and see nothing until they accept. What travels is the key that opens the calendar, encrypted to their key - so it has to be another Proton account.

They can read the calendar; --edit lets them change it too.

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

### `settings calendars share list`

List who has a calendar.

Somebody who has not answered yet is listed as pending: they were sent an invitation and cannot see anything until they take it.

```
proton calendar settings calendars share list REF
```

```bash
proton calendar settings calendars share list Work
```

### `settings calendars share remove`

Take somebody's access to a calendar away.

It works whether they accepted or not: an invitation nobody answered is withdrawn, and a membership somebody is using is ended.

```
proton calendar settings calendars share remove REF EMAIL
```

```bash
proton calendar settings calendars share remove Work jane@proton.me
```

### `settings calendars update`

Rename or recolor a calendar, or change what it gives new events.

The defaults are per-calendar because that is where Proton keeps them: a work calendar can open half-hour meetings with a reminder while a personal one does not.

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

### `settings get`

Show the calendar settings now in effect.

```
proton calendar settings get
```

```bash
proton calendar settings get
```

### `settings list`

List the calendar settings that can be changed.

```
proton calendar settings list
```

```bash
proton calendar settings list
```

### `settings set`

Change one calendar setting.

```
proton calendar settings set KEY VALUE
```

```bash
proton calendar settings set week-start monday
proton calendar settings set default-duration 30
```

---

Every command also takes the [flags that work everywhere](README.md#flags-that-work-on-every-command).
