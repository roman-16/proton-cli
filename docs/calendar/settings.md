# proton calendar settings

How Calendar behaves.

Every command under `proton calendar settings`, with the arguments and flags it takes. For these commands in use, see [the calendar guide](README.md).

Holds `calendars`, `get`, `holidays`, `links`, `list` and `set`.

## `calendars`

The calendars you keep events in.

Holds `create`, `delete`, `get`, `leave`, `list`, `share` and `update`.

### `calendars create`

Create a calendar, subscribe to one, or add public holidays.

--url takes the address of an .ics file. Proton fetches it on a schedule and fills the calendar from it, so those events are read-only. An address Proton cannot read is refused before the calendar is made.

--holidays takes a country by name or code, as `settings holidays list` shows them. The calendar takes the name Proton gives it, and its events are read-only. A country with holidays in more than one language needs --language.

```
proton calendar settings calendars create
```

```bash
proton calendar settings calendars create --name Work
proton calendar settings calendars create --name Personal --color pacific
proton calendar settings calendars create --name Timetable --url https://example.com/team.ics
proton calendar settings calendars create --holidays Austria
proton calendar settings calendars create --holidays Switzerland --language Français
```

| Flag | Description |
| --- | --- |
| `--color string` | Accent color, by name (purple) or hex (#8080FF) (default `#8080FF`) |
| `--holidays string` | Add this country's public holidays instead of making an empty calendar |
| `--language string` | Which language the holidays are in, for a country with more than one |
| `--name string` | Name for the new calendar |
| `--url string` | Subscribe to the calendar published at this address instead of making an empty one |

### `calendars delete`

Delete calendars, and every event in them.

Asks for your password even when you are signed in, except for a holidays calendar. With no terminal to ask, pass --password-file, which takes - for stdin. A calendar somebody shared with you is left with `leave` instead.

```
proton calendar settings calendars delete REF...
```

```bash
proton calendar settings calendars delete Work
```

| Flag | Description |
| --- | --- |
| `--password-file string` | Read the account password from a file, or - for stdin |
| `--totp string` | Two-factor code |

### `calendars get`

Show one calendar, with the defaults it gives new events.

```
proton calendar settings calendars get REF
```

```bash
proton calendar settings calendars get Work
```

### `calendars leave`

Leave calendars somebody shared with you.

Only the calendar's owner can give it to you again.

```
proton calendar settings calendars leave REF...
```

```bash
proton calendar settings calendars leave Team
```

### `calendars list`

List your calendars.

```
proton calendar settings calendars list
```

```bash
proton calendar settings calendars list
```

| Flag | Description |
| --- | --- |
| `--desc` | Reverse the order |
| `--limit int` | How many calendars per page; 0 for all of them |
| `--page int` | Which page of results, counting from zero |
| `--sort string` | Order by: name (default `name`) |

### `calendars share`

Who else can see a calendar.

Holds `add`, `get`, `remove` and `update`.

### `calendars share add`

Give somebody a calendar.

Only another Proton account can be given one.

They are sent an invitation and see nothing until they accept. A viewer reads the calendar; an editor changes it too.

```
proton calendar settings calendars share add REF EMAIL
```

```bash
proton calendar settings calendars share add Work jane@proton.me
proton calendar settings calendars share add Work jane@proton.me --access editor
```

| Flag | Description |
| --- | --- |
| `--access string` | What they may do with it: viewer, editor (default `viewer`) |

### `calendars share get`

Show who has a calendar.

Somebody who has not answered yet is listed as pending. They can see nothing until they accept.

```
proton calendar settings calendars share get REF
```

```bash
proton calendar settings calendars share get Work
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

### `calendars share update`

Change what somebody may do with a calendar.

Name them by address. It works whether they have accepted the calendar or still have it pending.

```
proton calendar settings calendars share update REF EMAIL
```

```bash
proton calendar settings calendars share update Work jane@proton.me --access viewer
```

| Flag | Description |
| --- | --- |
| `--access string` | What they may do with it: viewer, editor (default `viewer`) |

### `calendars update`

Rename or recolor a calendar, or change what it gives new events.

Defaults are set per calendar, so a work calendar can open half-hour meetings with a reminder while a personal one does not. --default-duration and --default apply only to a personal calendar of your own, and a holidays calendar takes only --color, --remind-all-day, --no-remind and --busy.

```
proton calendar settings calendars update REF
```

```bash
proton calendar settings calendars update Work --name Office
proton calendar settings calendars update Work --color enzian
proton calendar settings calendars update Work --default-duration 30m --remind 15m
proton calendar settings calendars update Personal --busy off
proton calendar settings calendars update Work --default
proton calendar settings calendars update 'Holidays in Austria' --remind-all-day 1d
```

| Flag | Description |
| --- | --- |
| `--busy string` | Whether events here make you look busy to others: on, off |
| `--color string` | New accent color, by name (purple) or hex (#8080FF) |
| `--default` | Make new events go here when nothing names another calendar |
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

## `holidays`

Public holidays calendars you can add.

Holds `list`.

### `holidays list`

List the public holidays calendars you can add.

These are what `calendars create --holidays` takes, by country name or code. A country with holidays in more than one language has a row for each, and ADDED says which of them you have.

```
proton calendar settings holidays list
```

```bash
proton calendar settings holidays list
proton calendar settings holidays list --output json
```

| Flag | Description |
| --- | --- |
| `--desc` | Reverse the order |
| `--limit int` | How many holidays calendars per page; 0 for all of them |
| `--page int` | Which page of results, counting from zero |
| `--sort string` | Order by: country (default `country`) |

## `links`

Links that open a calendar for anyone.

Holds `create`, `get`, `list`, `revoke` and `update`.

### `links create`

Publish a calendar as a link anyone can follow.

The link is an .ics feed, so any calendar app can follow it and stays up to date. A limited link shows only whether you are busy; a full one shows every detail of every event, and Proton reads them to serve it.

--name is yours alone and is never shown to anyone following the link. A calendar carries at most five links.

```
proton calendar settings links create REF
```

```bash
proton calendar settings links create Work
proton calendar settings links create Work --access full --name 'Team feed'
```

| Flag | Description |
| --- | --- |
| `--access string` | What they may do with it: limited, full (default `limited`) |
| `--name string` | Name for the new link, which only you see |

### `links get`

Show one link, URL and all.

Use this to recover a URL you mislaid, rather than revoking the link and making a new one. The URL appears here and in no listing.

```
proton calendar settings links get REF
```

```bash
proton calendar settings links get 'Team feed'
```

### `links list`

List the links you have published.

The URLs are not shown: each one opens its calendar for anybody holding it. To read a URL, use `links get`.

```
proton calendar settings links list
```

```bash
proton calendar settings links list
proton calendar settings links list --calendar Work
```

| Flag | Description |
| --- | --- |
| `--calendar string` | Which calendar, by name or ID (default: all of them) |
| `--desc` | Reverse the order |
| `--limit int` | How many links per page; 0 for all of them |
| `--page int` | Which page of results, counting from zero |
| `--sort string` | Order by: calendar, created, name (default `calendar`) |

### `links revoke`

Stop a link working.

The calendar is untouched; only the link stops opening it. Whatever somebody already copied out of it stays copied.

```
proton calendar settings links revoke REF...
```

```bash
proton calendar settings links revoke 'Team feed'
```

### `links update`

Rename a link.

The name is yours alone and is never shown to anyone following the link. --clear-name takes it off again.

What a link shows, and the URL it is opened at, cannot be changed: make another link for that.

```
proton calendar settings links update REF
```

```bash
proton calendar settings links update 'Team feed' --name 'Team, read-only'
proton calendar settings links update 'Team feed' --clear-name
```

| Flag | Description |
| --- | --- |
| `--clear-name` | Remove the link's name |
| `--name string` | New name, which only you see |

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
proton calendar settings set view week
proton calendar settings set primary-timezone Europe/Vienna
```

---

Every command also takes the [flags that work everywhere](../about/commands.md#flags-that-work-on-every-command).
