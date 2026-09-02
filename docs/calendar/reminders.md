# proton calendar reminders

The notifications your events will raise.

Every command under `proton calendar reminders`, with the arguments and flags it takes. For these commands in use, see [the calendar guide](README.md).

Holds `list` and `watch`.

## `list`

List every reminder your events will raise between two dates.

A reminder is listed on the day it goes off, not the day its event is on. An event with two reminders is two rows; a recurring event is one row per occurrence.

Emailed reminders are sent by Proton and are left out.

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

## `watch`

Print each reminder as it comes due, until you stop it.

Reminders land on the second. Calendars are re-read as it runs, so an event added while it is watching still reminds you.

Each line says what a desktop notification would say.

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

---

Every command also takes the [flags that work everywhere](../about/commands.md#flags-that-work-on-every-command).
