# Settings, files and environment

proton reads its settings from three places: a flag you pass, an environment variable, or one optional file. This page lists all three, plus the rules for which one wins.

For signing in, profiles and sessions, see [Account](../account/README.md).

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
| `no-log` | `--no-log`; `PROTON_NO_LOG` |
| `no-update-check` | `PROTON_NO_UPDATE_CHECK` |
| `confirm` | `--confirm`; `PROTON_CONFIRM` ([what it does](confirmations.md#making-more-commands-ask)) |

`profile` only works at the top level, because proton has to know which profile you are acting as before it can pick the section for it. Everything else may appear in either place.

**An unrecognised key is an error**, and so is a value of the wrong shape. Nothing runs until the file parses:

```console
$ proton mail messages list
Error: /home/you/.config/proton-cli/config.yaml: [4:1] unknown field "loglevel"
```

This is deliberate. The file carries your confirmation policy, and a policy that quietly fails to load is one that fails open.

### Settings the file rejects

| Setting | Why |
| --- | --- |
| `--yes` | A standing "yes" would cancel every confirmation you wrote, including a `deny`. Type it per run or not at all. |
| `--dry-run` | A file that turns every command into a preview looks exactly like work getting done. |
| `--verified` | It is a single token from a single refusal. It means nothing the next day. |
| `--api-url` | It is for developing proton, not using it. A durable way to point the CLI at a host that is not Proton is where credentials go missing. |

### Reading a different file

```console
$ proton --config ./ci.yaml mail messages list
$ PROTON_CONFIG=/etc/agents/proton.yaml proton mail drafts send 7fK2mQ
```

`--config` beats `PROTON_CONFIG`, which beats the default path.

A file you name and that is not there is an error. The default one being absent just means you have written no configuration.

## Which source wins

**For ordinary settings, the nearest source wins:**

```
--flag  >  environment variable  >  per-profile section  >  top level  >  built-in default
```

Which profile you are acting as is settled first, because the section depends on it:

```
--profile  >  PROTON_PROFILE  >  top-level profile:  >  default
```

**The confirmation policy is the exception. The strictest source wins:**

```
strongest of { built-in, top level, per-profile section, PROTON_CONFIRM, --confirm }
```

So nothing you write can make proton less careful than it is with no configuration at all. [Why this one is inverted](../about/why.md#why-the-confirmation-policy-resolves-the-other-way).

### Turning a file setting off for one run

A boolean you set in the file is still a preference, so pass the flag with an explicit value:

```console
$ proton --quiet=false mail messages list
```

## Environment variables

| Variable | What it does |
| --- | --- |
| `PROTON_CONFIG` | A config file somewhere other than the default path |
| `PROTON_CONFIRM` | The confirmation policy, on one line ([syntax](confirmations.md#writing-a-policy)) |
| `PROTON_PROFILE` | Active profile (default: `default`) |
| `PROTON_API_URL` | API base URL (default: `https://mail.proton.me/api`) |
| `NO_COLOR` | Set to any value, even empty, to turn colour off ([no-color.org](https://no-color.org)) |
| `COLORTERM` | Set to `truecolor` or `24bit` if your terminal takes 24-bit colour and does not advertise it. Only affects how exactly a colour swatch is drawn ([why](../about/why.md#why-colour-is-asked-for-by-name)) |
| `PROTON_NO_INPUT` | Set to any value, even empty, to never prompt. A missing credential becomes an error |
| `PROTON_LOG_LEVEL` | `debug`, `info`, `warn` or `error` |
| `PROTON_NO_LOG` | Set to any value, even empty, to write no diagnostic log ([what that is](#the-diagnostic-log)) |
| `TZ` | The IANA zone to work in, such as `Europe/Vienna`. POSIX's own variable, read when no flag or file names one |
| `PROTON_NO_UPDATE_CHECK` | Set to any value, even empty, to never look for a new release ([what that is](../install.md#updating)) |
| `PROTON_VERIFIED` | A human verification already solved, as the refusal printed it ([when you need it](../help/troubleshooting.md#solving-a-captcha-in-a-script)) |

## Files on disk

| Path | What is in it |
| --- | --- |
| `~/.config/proton-cli/config.yaml` | Your settings and confirmation policy. You write this one |
| `~/.config/proton-cli/sessions/<profile>.json` | Session tokens and the encrypted key password (mode `0600`) |
| `~/.config/proton-cli/idcache/<profile>.json` | Short-ID lookup table ([what that is](naming.md#short-ids)) |
| `~/.config/proton-cli/update-check.json` | When proton last looked for a new release ([why](../install.md#updating)) |
| `~/.config/proton-cli/logs/` | What every run did, one file per day ([below](#the-diagnostic-log)) |

Those are the Linux paths. macOS uses `~/Library/Application Support/proton-cli/`, and Windows uses `%APPDATA%\proton-cli\`. Nothing else is written.

## The diagnostic log

Every run writes what it did to `~/.config/proton-cli/logs/`, one file per day named for it, at full detail whatever `--log-level` says - that flag decides what reaches your screen, not what is recorded. The last **16 files** are kept.

It exists so that `proton report` can tell a maintainer what happened without you having to reproduce it. **Addresses, IDs, filenames and search terms never enter it.** An address is written as a stand-in like `address:3f9c1e@proton.me` - the same address reads the same way in every line, so a reader can follow which one failed, and it cannot be turned back into an address by anybody. What is left is the shape of what ran: which command, which endpoints, which status codes, how long, and what went wrong.

To write nothing at all:

```console
$ proton --no-log mail messages list
$ PROTON_NO_LOG=1 proton mail messages list
```

```yaml
no-log: true
```

With the log off, `proton report` has nothing to show but this build and your settings.

## Global flags

Every command takes these.

| Flag | What it does |
| --- | --- |
| `-p`, `--profile NAME` | Which profile to act as |
| `-o`, `--output text\|json\|yaml` | Output format (default `text`) |
| `-n`, `--dry-run` | Preview a mutation without applying it |
| `-y`, `--yes` | Answer confirmation prompts with yes ([when a script needs it](confirmations.md#in-a-script)) |
| `-q`, `--quiet` | Suppress the non-essential stderr output |
| `--config PATH` | Settings file to read (env: `PROTON_CONFIG`) |
| `--confirm POLICY` | Which commands stop for a yes (env: `PROTON_CONFIRM`) |
| `--full-ids` | Do not shorten IDs in interactive output |
| `--no-color` | Turn colour off (env: `NO_COLOR`) |
| `--log-level debug\|info\|warn\|error` | Logging verbosity (env: `PROTON_LOG_LEVEL`) |
| `--zone NAME` | IANA time zone to work in (env: `TZ`) |
| `--no-input` | Never prompt. A missing credential becomes an error (env: `PROTON_NO_INPUT`) |
| `--no-log` | Write no diagnostic log for this run (env: `PROTON_NO_LOG`) |
| `--verified TOKEN` | A human verification already solved, as the refusal printed it (env: `PROTON_VERIFIED`) |

The five you type most have a single-letter form, and they cluster, so `-qn` is a quiet dry run. [Why only five](../about/why.md#why-one-flag-name-means-one-thing).

`--api-url URL` points the CLI at a different API host. It works, but it is hidden from `--help` because it is for developing proton rather than using it.
