# proton calendar booking-pages

Pages where people book a time with you.

Every command under `proton calendar booking-pages`, with the arguments and flags it takes. For these commands in use, see [the calendar guide](README.md).

Holds `create`, `delete`, `get`, `list` and `update`.

## `create`

Make a booking page and print its link.

Anyone with the link can see when you are free and book an appointment, which lands in --calendar as an event.

--available is when appointments can be booked, as DAY=START-END, repeatable. A weekday repeats every week; a date is that day alone, and a page is one or the other:

  mon=09:00-17:00         every Monday
  mon-fri=09:00-12:00     every weekday morning
  mon,wed=14:00-18:00     every Monday and Wednesday afternoon
  2026-10-05=09:00-12:00  that morning only

Without it, a page offers Monday to Friday, 09:00-17:00, every week. Times are read in your zone, and 24:00 ends a window at midnight.

Without --duration, an appointment lasts what a new event lasts in --calendar. Without --location, each appointment gets a Proton Meet link. A page offers at most 200 appointments. How many pages you may have depends on your plan; a free plan allows none.

```
proton calendar booking-pages create
```

```bash
proton calendar booking-pages create --title 'Intro call'
proton calendar booking-pages create --title 'Office hours' --calendar Work --duration 1h --available tue=14:00-18:00 --notice 48h
proton calendar booking-pages create --title 'Site visit' --available 2026-10-05=09:00-12:00 --available 2026-10-06=14:00-17:00 --location 'Hauptplatz 1, Graz'
```

| Flag | Description |
| --- | --- |
| `--available stringArray` | When appointments can be booked, as DAY=START-END (repeatable) |
| `--blocked-by stringArray` | Another calendar whose events keep a time from being booked, by name or ID (repeatable) |
| `--calendar string` | Which calendar bookings go into, by name or ID (default: your default calendar) |
| `--description string` | Set what people read before they book |
| `--duration string` | Set how long an appointment lasts: 15m, 30m, 1h, 1h30m, 2h |
| `--location string` | Set where appointments take place (default: a Proton Meet link) |
| `--notice string` | Set how soon before an appointment it can still be booked: none, 2h, 48h, next-day (default `none`) |
| `--title string` | Set the title |

## `delete`

Delete booking pages.

The link stops working. Appointments already booked stay in your calendar.

```
proton calendar booking-pages delete REF...
```

```bash
proton calendar booking-pages delete 'Intro call'
```

## `get`

Show one booking page, link and all.

The link appears here and when the page is made, never in a listing.

```
proton calendar booking-pages get REF
```

```bash
proton calendar booking-pages get 'Intro call'
proton calendar booking-pages get 'Intro call' --output json
```

## `list`

List your booking pages.

The links are not shown; `booking-pages get` shows one.

```
proton calendar booking-pages list
```

```bash
proton calendar booking-pages list
proton calendar booking-pages list --sort created
```

| Flag | Description |
| --- | --- |
| `--desc` | Reverse the order |
| `--limit int` | How many booking pages per page; 0 for all of them |
| `--page int` | Which page of results, counting from zero |
| `--sort string` | Order by: title, calendar, created (default `title`) |

## `update`

Change what a booking page offers.

Anything you do not mention is left alone. --available replaces every window, read in your zone; --duration alone re-cuts the windows the page offers now. --blocked-by replaces the list and --no-blocked-by empties it. The calendar a page books into, and its link, cannot be changed.

```
proton calendar booking-pages update REF
```

```bash
proton calendar booking-pages update 'Intro call' --duration 1h --notice 2h
proton calendar booking-pages update 'Intro call' --available mon-fri=09:00-12:00 --blocked-by Work
proton calendar booking-pages update 'Intro call' --location 'Café Central'
proton calendar booking-pages update 'Intro call' --meet --no-blocked-by
```

| Flag | Description |
| --- | --- |
| `--available stringArray` | Replace when appointments can be booked, as DAY=START-END (repeatable) |
| `--blocked-by stringArray` | Replace the other calendars whose events keep a time from being booked (repeatable) |
| `--description string` | Replace what people read before they book |
| `--duration string` | Replace how long an appointment lasts: 15m, 30m, 1h, 1h30m, 2h |
| `--location string` | Replace where appointments take place |
| `--meet` | Give each appointment a Proton Meet link instead of a place |
| `--no-blocked-by` | Let only the page's own calendar keep times from being booked |
| `--notice string` | Replace how soon before an appointment it can still be booked: none, 2h, 48h, next-day |
| `--title string` | Replace the title |

---

Every command also takes the [flags that work everywhere](../about/commands.md#flags-that-work-on-every-command).
