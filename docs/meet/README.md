# Meet

Your Proton Meet meetings and rooms, your personal room, and the links people join them by. Anyone holding a link can join, so a listing never shows one.

This page is what people actually do. For every command and flag, see the reference: [meetings](meetings.md), [rooms](rooms.md).

## See your meetings and rooms

```bash
proton meet meetings list
proton meet rooms list
proton meet meetings get "Weekly sync"
```

A meeting that repeats is listed at its next occurrence. `get` shows the link, beside a warning that anyone holding it can join.

`REF` is the ID a link shows after `id-`, the whole link, or the title. `personal` names your personal room. A meeting made for a calendar event is not listed, and opens by its whole link.

## Schedule a meeting

```bash
proton meet meetings create --title "Weekly sync" --start 2026-11-04T09:00 --rrule FREQ=WEEKLY
proton meet meetings create --title Interview --start 2026-11-05T14:00 --duration 1h
```

`--start` is read in your zone. Without `--end` or `--duration` a meeting lasts 30 minutes. The link is printed with the meeting.

A meeting made here is in Proton Meet alone. To invite people with a link, put it on a calendar event instead, as [Calendar](../calendar/README.md#proton-meet-links) shows.

## Move, rename or delete a meeting

```bash
proton meet meetings update "Weekly sync" --start 2026-11-04T10:00
proton meet meetings update "Weekly sync" --title "Team sync" --rrule ""
proton meet meetings delete "Team sync"
```

A moved meeting keeps its length unless `--end` or `--duration` says otherwise. `--rrule ""` makes it happen once.

A meeting that belongs to a calendar event changes with the event, through `proton calendar events update`. Deleting it leaves the event with a link that no longer works.

## Rooms

A room has no time, and its link works until you delete it.

```bash
proton meet rooms create --title "Team room"
proton meet rooms update "Team room" --title "Platform room"
proton meet rooms delete "Platform room"
```

## Your personal room

```bash
proton meet rooms get personal
proton meet rooms rotate personal
```

`rotate` gives your personal room a new link, and the old one stops working. It can change once every 24 hours. Your personal room cannot be deleted.
