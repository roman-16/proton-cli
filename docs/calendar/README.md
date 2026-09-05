# Calendar

Events, recurrence, reminders and invitations in Proton Calendar, plus `.ics` in and out. Everything is encrypted with your calendar key and signed with your address key.

This page is what people actually do. For every command and flag, see the reference: [events](events.md), [reminders](reminders.md), [invitations](invitations.md), [settings](settings.md).

## See what's on

```bash
proton calendar events list
proton calendar events list --calendar Work --start 2026-04-15 --end 2026-04-30
proton calendar events get "Team sync"
```

Every calendar is included unless `--calendar` narrows it.

`--start` and `--end` are the first and last **whole** days to include, read in your own zone. Without them you get the next 30 days.

An event is on a day when it touches any part of it, so a query for one day inside a three-day event returns it.

Each occurrence of a recurring event is listed on its own day, with a reference naming that occurrence:

```console
$ proton calendar events list --start 2026-04-20 --end 2026-04-27
ID                         DATE        TIME     DURATION  TITLE           LOCATION
─────────────────────────  ──────────  ───────  ────────  ──────────────  ────────
4f2a1b9c@2026-04-20T09:00  2026-04-20  09:00    15m       Standup         Meet
7bd3e011                   2026-04-21  all day  1d        Public holiday
4f2a1b9c@2026-04-22T10:30  2026-04-22  10:30    30m       Standup (long)  Meet
4f2a1b9c@2026-04-27T09:00  2026-04-27  09:00    15m       Standup         Meet
```

## Create an event

```bash
proton calendar events create --title Dentist --start 2026-04-16T14:00 --duration 1h
proton calendar events create --title Conference --start 2026-04-20 --all-day --duration 3d
proton calendar events create --calendar Work --title "Quarterly sync" \
  --start 2026-04-16T14:00 --duration 90m --location "Vienna HQ" --description "Numbers and roadmap"
```

Say how long an event lasts **once**, with either `--end` or `--duration`. Both together is refused, and so is either without `--start`. Say neither and a new event lasts as long as its calendar says a new event lasts - `settings calendars get` shows that, `settings calendars update --default-duration` sets it - while an all-day event lasts a day.

| Flag | Accepts |
| --- | --- |
| `--start` | `2026-04-16T14:00`, `2026-04-16 14:00`, `2026-04-16`, or full RFC 3339 |
| `--end` | the last day it runs through (`2026-04-22`), or a day and a time |
| `--duration` | `15m`, `90m`, `1h`, `2h30m`, or `3d` for an all-day event |
| `--remind` | `15m`, `1h`, `1d`, repeatable; add `:email` for an emailed one |
| `--start` / `--end` on `list` | `YYYY-MM-DD`, both days included |

`--all-day` makes an event with no time of day, measured in days. It ends at the midnight after its last day, which is how every other calendar client writes it.

Whether an event has a time of day is what a written time says it is. A bare day on `create` needs `--all-day` to agree; a day and a time on `update` gives an all-day event a time of day, and a bare day moves an event and leaves its time of day alone. `--all-day` takes one away, and `--all-day=false` gives one back - it needs a `--start` saying which time, since nothing else could.

```bash
proton calendar events update Offsite --all-day
proton calendar events update Offsite --all-day=false --start 2026-07-01T09:00 --end 2026-07-01T17:00
```

`--status` says whether the event is going ahead: `confirmed`, `tentative` or `cancelled`. Cancelling this way keeps the event and its history, which `delete` does not.

### Colour

`--color` gives one event a colour of its own, on `create` and on `update`. An event without one is drawn in its calendar's colour.

```bash
proton calendar events create --title Deadline --start 2026-04-30T17:00 --duration 1h --color strawberry
proton calendar events update Dentist --color pacific
```

Name one of Proton's twenty accent colours or give its hex value. Anything else is refused before the request goes out, and the refusal prints the palette.

An edit that says nothing about the colour leaves it alone, and a colour reaches exactly what the reference names: one occurrence takes its own, `--onwards` colours that one and every later one, and the series' own reference colours all of it.

A colour cannot be taken away once an event has one - it can only be changed. Proton has no value meaning "none", and its own apps offer no way back either.

A colour per event is a paid feature, and Proton enforces it when drawing rather than when storing. A free account's colour is stored, and `events get` reports it, but Proton's own apps draw the calendar's.

### Attendees

Proton users are added directly. External addresses get an emailed invitation.

A bare address is required: `--attendee jane@example.com:optional` is not accepted.

### Recurrence

`--rrule` takes an iCal recurrence rule.

```bash
proton calendar events create --title Standup --start 2026-04-16T09:00 --duration 15m \
  --rrule "FREQ=WEEKLY;COUNT=10" --remind 15m
```

The series is anchored to the zone you are working in, so a 09:00 standup stays at 09:00 when the clocks change. One stored as a plain UTC instant would slide to 08:00.

### Time zones

Every event carries the zone it is anchored to, and the result says which:

```console
$ proton calendar events create --title Dentist --start 2026-04-16T14:00
✓ Created event "Dentist" for 2026-04-16 14:00 Europe/Vienna.
```

The zone comes from `--zone`, `TZ`, `zone:` in your [config](../using/settings.md), your system, or your Proton calendar's primary zone, in that order.

The four hours a year where a clock reading means two instants or none are refused rather than guessed. Add an offset to say which you meant:

```console
$ proton calendar events create --title 'Night shift' --start 2026-10-25T02:30
Error: 02:30 happens twice on 2026-10-25 in Europe/Vienna, when the clocks go back; say which one, as 2026-10-25T02:30:00+02:00 or 2026-10-25T02:30:00+01:00
```

[Why an ambiguous time is refused](../about/why.md#why-an-ambiguous-time-is-refused-rather-than-resolved).

## Change or answer one

```bash
proton calendar events update REF --title "New title" --start 2026-04-17T10:00 --duration 2h
proton calendar events respond "Team sync" --answer accept     # emails the organizer
proton calendar events delete "Dentist"
```

Anything you do not mention is left alone, including the reminders, the recurrence and the occurrences you have cancelled.

`--answer` is `accept`, `tentative` or `decline`, and applies to the whole invitation rather than to one occurrence.

## Recurring events, occurrence by occurrence

**The reference decides how far a change reaches.** Keep the `@occurrence` part to act on that occurrence. Drop it to act on the series. `--onwards` widens one occurrence to it and everything after.

```bash
# move one standup, leaving the series alone
proton calendar events update 4f2a1b9c@2026-04-22T09:00 --start 2026-04-22T10:30 --duration 30m

# cancel one standup
proton calendar events delete 4f2a1b9c@2026-04-22T09:00

# from May the 4th on, it moves half an hour later
proton calendar events update 4f2a1b9c@2026-05-04T09:00 --start 2026-05-04T09:30 --onwards

# end the series there
proton calendar events delete 4f2a1b9c@2026-05-04T09:00 --onwards

# the whole series
proton calendar events delete 4f2a1b9c
```

A bare reference reaches every occurrence a series holds, so both `update` and `delete` say how many and show the first of them:

```console
$ proton calendar events update Standup --start 2026-04-16T10:00 --dry-run
Dry run - would update event "Standup" and all 500 occurrences of it, now 2026-04-16 10:00 Europe/Vienna:

ID                                DATE        TIME   DURATION  TITLE    LOCATION
────────────────────────────────  ──────────  ─────  ────────  ───────  ────────
Fh3jAe…/9xL4pQ…@2026-04-16T09:00  2026-04-16  09:00  15m       Standup  Room 2
…
```

A series with no end says so instead of giving a number ([why](../about/why.md#why-an-occurrence-count-is-a-number-or-nothing)).

`--onwards` on the *first* occurrence is refused, because nothing would be left. Delete the series instead.

## Reminders

`events list` shows which triggers an event carries. `reminders list` answers the other question, which is when they go off:

```console
$ proton calendar reminders list --start 2026-08-27 --end 2026-08-28
ID                         FIRES             REMIND  TITLE    STARTS              LOCATION
─────────────────────────  ────────────────  ──────  ───────  ──────────────────  ────────
7bd3e011                   2026-08-27 06:00  6h      Piano    2026-08-27 all day
4f2a1b9c@2026-08-27T09:00  2026-08-28 08:45  15m     Standup  2026-08-28 09:00    Room 3
```

A reminder is listed on the day it *fires*, not the day its event is on. An event with two triggers is two rows, and a recurring one is a row per occurrence.

An all-day event's reminder goes off at the calendar's chosen morning hour, whatever trigger produced it.

Emailed reminders are sent by Proton and are left out.

The same rows, live:

```bash
proton calendar reminders watch --output json | jq -c .
```

`watch` lands a line on the second, and re-reads your calendars as it runs, so an event added while it is watching still reminds you.

The last column is the sentence a notification would say, which is `says` in JSON. See [Desktop notifications](../using/scripting.md#desktop-notifications).

## Import and export

```bash
proton calendar events export --start 2026-01-01 --end 2026-12-31 --dest year.ics
proton calendar events import holidays.ics
curl -s https://example.com/team.ics | proton calendar events import -
```

A recurring series is exported **once** with its rule rather than expanded, so another client reads it back as the same series. Reminders travel as `VALARM` components. An event that cannot be decrypted is left out rather than written as a stub.

An event's own colour travels as a `COLOR` property, so a round trip keeps it. On the way in, a colour the file names in CSS - `tomato`, `#FF6347` - becomes the nearest of Proton's twenty accents, since those are the only colours it stores.

**An import is addressed by UID.** An event carries the UID of the event it is, so reading a file back changes that event rather than making a second one. Export, edit, import, and the calendar says what the file says.

The replacement is a new event with a new ID, so a reference held from before no longer resolves. Use `--dry-run` to list what the file holds first.

**Participants are left out.** An imported event is a record, not an invitation being reissued. Writing the guests back would make your account the organizer of a meeting it did not call, which for external addresses means email going out.

## Your calendars

```bash
proton calendar settings calendars create --name Work --color "#8080FF"
proton calendar settings calendars update Work --default-duration 30m --remind 15m
proton calendar settings calendars update Personal --busy off
proton calendar settings calendars delete Work
```

Each calendar carries its own defaults for the events made in it.

- `--busy` says whether events there make you look busy to people checking your availability.
- `--no-remind` gives new events none.

Colours have to be Proton accent colours.

**Deleting a calendar asks for your password** even though you are signed in, because Proton guards that endpoint behind an elevated session. With no terminal, pass `--password-file` or `--password-stdin`.

### Subscribe to a published calendar

```bash
proton calendar settings calendars create --name Timetable --url https://example.com/team.ics
```

Proton fetches the `.ics` on a schedule and fills the calendar from it, so the events are **read-only**: they belong to whoever publishes them. A listing says which calendars are which under `KIND`.

Proton is asked whether it can read the address before the calendar is made, so a wrong one is refused rather than leaving you with a calendar that never fills.

### Share a calendar

```bash
proton calendar settings calendars share add Work jane@proton.me --edit
proton calendar settings calendars share list Work
proton calendar settings calendars share remove Work jane@proton.me
```

**It only works with another Proton account.** A calendar is opened by a passphrase, and every member holds that passphrase encrypted to their own key. So sharing is not a permission Proton grants: it is handing somebody the key, encrypted so only they can read it and signed so they can tell it came from you. An address Proton holds no keys for has nothing to encrypt to.

They see nothing until they accept, and until then `share list` shows them as `pending`. `share remove` withdraws an unanswered invitation or ends a membership.

For a calendar somebody gave you:

```bash
proton calendar invitations list
proton calendar invitations accept Work
```

Until you accept, you see the calendar's name and who sent it, and nothing that is on it.

## Settings

```bash
proton calendar settings          # time zones, layout, invitations
proton calendar settings set view week
proton calendar settings set primary-timezone Europe/Vienna
```

| Key | Values |
| --- | --- |
| `view` | `day`, `week`, `month`, `year`, `planning` |
| `week-numbers` · `auto-detect-timezone` · `show-secondary-timezone` · `auto-import-invite` | `off`, `on` |
| `primary-timezone` · `secondary-timezone` | An IANA zone |
| `invite-locale` | A language, such as `en_US` |
| `default-calendar` | A calendar ID |
