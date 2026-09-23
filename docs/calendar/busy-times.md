# proton calendar busy-times

When other people are busy.

Every command under `proton calendar busy-times`, with the arguments and flags it takes. For these commands in use, see [the calendar guide](README.md).

Holds `list`.

## `list`

List when people are busy in a date range.

With neither --after nor --before it covers the next 7 days, starting today. With one of them, it covers the 7 days starting or ending there.

It needs a Duo, Family, Visionary or business plan. Somebody whose busy times Proton will not show, such as anyone outside Proton, is listed as unknown. A busy time says when, never what.

```
proton calendar busy-times list EMAIL...
```

```bash
proton calendar busy-times list jane.roe@example.com
proton calendar busy-times list jane.roe@example.com alex.roe@example.com --after 2026-05-04 --before 2026-05-08
proton calendar busy-times list jane.roe@example.com --output json
```

| Flag | Description |
| --- | --- |
| `--after string` | First day to include (YYYY-MM-DD) |
| `--before string` | Last day to include (YYYY-MM-DD) |
| `--limit int` | How many busy times per page; 0 for all of them |
| `--page int` | Which page of results, counting from zero |

---

Every command also takes the [flags that work everywhere](../about/commands.md#flags-that-work-on-every-command).
