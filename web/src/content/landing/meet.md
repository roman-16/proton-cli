---
gradient: var(--app-meet)
href: /meet/
order: 5
summary: Meetings and rooms, your personal room, and Proton Meet links on calendar events.
title: Meet
---

```bash frame="none"
proton meet meetings create --title "Weekly sync" --start 2026-11-04T09:00 --rrule FREQ=WEEKLY
proton meet rooms get personal
proton calendar events create --title "Design review" --start 2026-11-03T10:00 --meet
proton meet meetings list
```
