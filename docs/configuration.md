# The config file, environment variables and global flags

Everything proton-cli reads comes from a flag you pass, an environment variable, or one optional file - and this page is the complete list of all three, plus the rules for which of them wins.

For signing in, profiles and sessions, see [Your Proton account](apps/account.md).

## The config file

`~/.config/proton-cli/config.yaml`. It does not exist until you write it, and nothing needs it.

```yaml
profile: work

output: json
log-level: warn

confirm:
  ask:
    "*": mutations

per-profile:
  work:
    confirm:
      deny:
        "*": deletions
  personal:
    output: text
    no-color: true
```

The top level applies whichever profile you are acting as. `per-profile:` narrows a setting to one of them.

| Key | Also settable as |
| --- | --- |
| `profile` | `-p`, `--profile`; `PROTON_PROFILE` |
| `output` | `-o`, `--output` |
| `quiet` | `-q`, `--quiet` |
| `log-level` | `--log-level`; `PROTON_LOG_LEVEL` |
| `zone` | `--zone`; `TZ` |
| `full-ids` | `--full-ids` |
| `no-color` | `--no-color`; `NO_COLOR` |
| `no-input` | `--no-input`; `PROTON_NO_INPUT` |
| `no-update-check` | `PROTON_NO_UPDATE_CHECK` |
| `confirm` | `--confirm`; `PROTON_CONFIRM` ([what it does](language.md#when-it-asks-first)) |

`profile` only means something at the top level, because which profile you are acting as has to be known before the section for it can be chosen. Everything else may appear in either place.

**A key it does not recognise is an error**, and so is a value of the wrong shape. Nothing runs until the file parses:

```console
$ proton mail messages list
Error: /home/you/.config/proton-cli/config.yaml: [4:1] unknown field "loglevel"
```

That is deliberate. The file carries your confirmation policy, and a policy that quietly fails to load is one that fails open.

### Four things it will not hold

| Setting | Why |
| --- | --- |
| `--yes` | A standing "yes" cancels every confirmation you wrote, including a `deny`. It is typed per run or not at all. |
| `--dry-run` | A file that turns every command into a preview looks exactly like work getting done. |
| `--verified` | A single token from a single refusal; it means nothing the next day. |
| `--api-url` | It is for developing proton, not using it. A durable way to point the CLI at a host that is not Proton is where credentials go missing. |

### Reading a different file

```console
$ proton --config ./ci.yaml mail messages list
$ PROTON_CONFIG=/etc/agents/proton.yaml proton mail drafts send 7fK2mQ
```

`--config` beats `PROTON_CONFIG` beats the default path. A file you name and that is not there is an error; the default one being absent is just having written no configuration.

## What wins

**Ordinary settings** take the nearest answer:

```
--flag  >  environment variable  >  per-profile section  >  top level  >  built-in default
```

Which profile you are acting as is settled first, because the section depends on it:

```
--profile  >  PROTON_PROFILE  >  top-level profile:  >  default
```

**The confirmation policy is the exception**, and takes the most cautious answer instead:

```
strongest of { built-in, top level, per-profile section, PROTON_CONFIRM, --confirm }
```

Every other setting says how proton should behave for your convenience, so the nearest source is the right one. That one says what it must not do by accident, so the most careful source is - a guard a nearer source can lower is not a guard. Nothing you can write makes proton less careful than it is with no configuration at all ([the reasoning](design-notes.md#why-the-confirmation-policy-resolves-the-other-way)).

### Turning a file setting off for one run

A boolean you set in the file is still a preference, so pass the flag with an explicit value:

```console
$ proton --quiet=false mail messages list
```

## Environment variables

| Variable | Description |
| --- | --- |
| `PROTON_CONFIG` | A config file somewhere other than the default path |
| `PROTON_CONFIRM` | The confirmation policy, on one line ([syntax](language.md#writing-a-policy)) |
| `PROTON_PROFILE` | Active profile (default: `default`) |
| `PROTON_API_URL` | API base URL (default: `https://mail.proton.me/api`) |
| `NO_COLOR` | Set to any value, even empty, to turn colored output off ([no-color.org](https://no-color.org)) |
| `COLORTERM` | `truecolor` or `24bit` if your terminal takes 24-bit color and does not advertise it; only affects how exactly a color swatch is drawn ([why](design-notes.md#why-colour-is-asked-for-by-name)) |
| `PROTON_NO_INPUT` | Set to any value, even empty, to never prompt; a missing credential becomes an error |
| `PROTON_LOG_LEVEL` | `debug`, `info`, `warn` or `error` |
| `TZ` | The IANA zone to work in, e.g. `Europe/Vienna`; POSIX's own variable, read when no flag or file names one |
| `PROTON_NO_UPDATE_CHECK` | Set to any value, even empty, to never look for a new release ([what that is](installation.md#updating)) |
| `PROTON_VERIFIED` | A human verification already solved, as the refusal printed it ([when you need it](troubleshooting.md#nothing-here-can-be-asked)) |

## Files on disk

| Path | Contents |
| --- | --- |
| `~/.config/proton-cli/config.yaml` | Your settings and confirmation policy; you write this one |
| `~/.config/proton-cli/sessions/<profile>.json` | Session tokens and the encrypted key password (mode `0600`) |
| `~/.config/proton-cli/idcache/<profile>.json` | Short-ID lookup table (see [Short IDs](language.md#short-ids)) |
| `~/.config/proton-cli/update-check.json` | When proton last looked for a new release ([why](installation.md#updating)) |

Those paths are the Linux ones. macOS uses `~/Library/Application Support/proton-cli/` and Windows uses `%APPDATA%\proton-cli\`. Nothing else is written.

## Global flags

| Flag | Effect |
| --- | --- |
| `-p`, `--profile NAME` | Which profile to act as |
| `-o`, `--output text\|json\|yaml` | Output format (default `text`) |
| `-n`, `--dry-run` | Preview a mutation without applying it |
| `-y`, `--yes` | Answer confirmation prompts with yes; needed by a script that removes things ([why](language.md#when-it-asks-first)) |
| `-q`, `--quiet` | Suppress the non-essential stderr output |
| `--config PATH` | Settings file to read (env: `PROTON_CONFIG`) |
| `--confirm POLICY` | Which commands stop for a yes (env: `PROTON_CONFIRM`) |
| `--full-ids` | Don't shorten IDs in interactive output |
| `--no-color` | Turn colored output off (env: `NO_COLOR`) |
| `--log-level debug\|info\|warn\|error` | Logging verbosity (env: `PROTON_LOG_LEVEL`) |
| `--zone NAME` | IANA time zone to work in (env: `TZ`) |
| `--no-input` | Never prompt; a missing credential becomes an error (env: `PROTON_NO_INPUT`) |
| `--verified TOKEN` | A human verification already solved, as the refusal printed it (env: `PROTON_VERIFIED`) |

The five you type most have a single-letter form, and they cluster - so `-qn` is a quiet dry run ([why only five](design-notes.md#why-one-flag-name-means-one-thing)).

`--api-url URL` points the CLI at a different API host. It works but is hidden from `--help`, because it is for developing proton rather than for using it.
