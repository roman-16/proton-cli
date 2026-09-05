# proton mail drafts

Messages not yet sent.

Every command under `proton mail drafts`, with the arguments and flags it takes. For these commands in use, see [the mail guide](README.md).

Holds `create`, `delete`, `list`, `send` and `update`.

## `create`

Save a draft without sending it.

```
proton mail drafts create
```

```bash
proton mail drafts create --to team@example.com --subject Standup --body 'Notes to follow.'
proton mail drafts create --to jane@example.com --subject Report --attach ./report.pdf
```

| Flag | Description |
| --- | --- |
| `--attach stringArray` | File to attach (repeatable) |
| `--attach-inline stringArray` | Image to embed in the HTML body by Content-ID (repeatable; needs --html) |
| `--bcc stringArray` | Blind-carbon-copy recipient (repeatable) |
| `--body string` | Message body (- reads stdin) |
| `--cc stringArray` | Carbon-copy recipient (repeatable) |
| `--eml string` | Build the message from an RFC 822 file; other flags override what it says |
| `--from string` | Address to send from, by email or ID (default: your primary) |
| `--html` | Treat the body as HTML rather than plain text |
| `--no-signature` | Leave out this address's signature and Proton's footer |
| `--subject string` | Subject line |
| `--to stringArray` | Recipient (repeatable; accepts "Name <addr>") |

## `delete`

Delete drafts.

```
proton mail drafts delete REF...
```

```bash
proton mail drafts delete 5bH2mQxK
```

## `list`

List drafts.

```
proton mail drafts list
```

```bash
proton mail drafts list
```

| Flag | Description |
| --- | --- |
| `--page int` | Which page of results, counting from zero |
| `--page-size int` | How many drafts per page; 0 for all of them (default `25`) |

## `send`

Send a draft as it stands.

No signature is appended: the draft already holds whatever signature it was created with.

```
proton mail drafts send REF
```

```bash
proton mail drafts send 5bH2mQxK
proton mail drafts send 5bH2mQxK --send-at 2026-04-16T09:00
```

| Flag | Description |
| --- | --- |
| `--eo-password-file string` | Read the password for recipients outside Proton from a file |
| `--eo-password-hint string` | Hint shown to password-protected recipients |
| `--eo-password-stdin` | Read the password for recipients outside Proton from stdin |
| `--expires string` | Self-destruct after DURATION (e.g. 7d, 24h), or never |
| `--send-at string` | Schedule delivery (RFC 3339, or YYYY-MM-DDTHH:MM in the zone you are working in) |

## `update`

Change a draft. Only what you pass is replaced; everything else is kept.

--to, --cc and --bcc replace the whole list rather than adding to it. --attach adds files and --detach removes one by name or ID.

```
proton mail drafts update REF
```

```bash
proton mail drafts update 5bH2mQxK --body 'Notes attached.'
proton mail drafts update 5bH2mQxK --detach report.pdf
```

| Flag | Description |
| --- | --- |
| `--attach stringArray` | File to attach (repeatable) |
| `--attach-inline stringArray` | Image to embed in the HTML body by Content-ID (repeatable; needs --html) |
| `--bcc stringArray` | Blind-carbon-copy recipient (repeatable) |
| `--body string` | Message body (- reads stdin) |
| `--cc stringArray` | Carbon-copy recipient (repeatable) |
| `--detach stringArray` | Remove an attachment by name or ID (repeatable) |
| `--from string` | Address to send from, by email or ID (default: your primary) |
| `--html` | Switch the draft to text/html |
| `--no-signature` | Leave out this address's signature and Proton's footer |
| `--plain` | Switch the draft to text/plain |
| `--subject string` | Subject line |
| `--to stringArray` | Recipient (repeatable; accepts "Name <addr>") |

---

Every command also takes the [flags that work everywhere](../about/commands.md#flags-that-work-on-every-command).
