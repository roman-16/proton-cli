# Command reference

Every command, every argument and every flag, generated from the command tree.

```
proton <app> <collection> <verb> [TARGET...] [--flags]
```

Anywhere a command shows `REF`, you can pass a full ID, the eight-character short ID a list printed, or something human: a subject, a name, a path, an email address. See [Naming the thing you want](../language.md#naming-the-thing-you-want).

## Apps

- **[`proton calendar`](calendar.md)** - calendars and events. 24 commands.
- **[`proton contacts`](contacts.md)** - contacts, their groups and their pinned keys. 18 commands.
- **[`proton drive`](drive.md)** - files and folders in Drive. 45 commands.
- **[`proton mail`](mail.md)** - read, write and organize mail. 77 commands.
- **[`proton pass`](pass.md)** - vaults, logins and secrets. 60 commands.

## Account

- **[`proton account`](account.md)** - your Proton account, its settings and your session. 10 commands.
- **[`proton api`](api.md)** - send a raw authenticated request to the Proton API. 1 command.

## proton itself

- **[`proton` itself](self.md)** - updating, uninstalling, completions and what a release changed. 5 commands.

## Flags that work on every command

These are declared on the root, so they can be given to any command and mean the same thing on all of them.

| Flag | Description |
| --- | --- |
| `--config string` | Settings file to read (env: PROTON_CONFIG; default: config.yaml in the config directory) |
| `--confirm string` | Which commands stop for a yes: default, deletions, mutations, reads, all (env: PROTON_CONFIRM) |
| `-n, --dry-run` | Preview mutations without applying them |
| `--full-ids` | Show full IDs in interactive output (default: shortened to 8 chars on TTY) |
| `--log-level string` | Logging verbosity: debug, info, warn, error (env: PROTON_LOG_LEVEL) |
| `--no-color` | Disable colored output (env: NO_COLOR) |
| `--no-input` | Never prompt; a missing credential becomes an error (env: PROTON_NO_INPUT) |
| `-o, --output string` | Output format: text, json, yaml (default "text") |
| `-p, --profile string` | Profile to act as (env: PROTON_PROFILE; default: default) |
| `-q, --quiet` | Suppress non-essential stderr output |
| `--verified string` | A human verification already solved, as the refusal printed it (env: PROTON_VERIFIED) |
| `-y, --yes` | Answer confirmation prompts with yes |

See [Configuration](../configuration.md) for what each one changes, and [Output](../output.md) for the exit codes.
