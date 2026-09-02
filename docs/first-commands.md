# First commands

Sign in once, then read your mail, move files in and out of Drive, check your calendar and pull a two-factor code. This is the five minutes from a fresh install to a useful command.

## Sign in

```console
$ proton account login
Email:            you@proton.me
Password:
Two-factor code:  123456

✓ Signed in as you@proton.me.
```

That is the whole setup. Signing in saves the session **and** unlocks your keys, so your password is needed once on this machine and not again.

If there is no terminal to answer the prompts, name the account and point at the password ([never a flag value](about/why.md#why-a-password-is-never-a-flag-value)):

```bash
proton account login --user you@proton.me --password-file /run/secrets/proton
```

## Check where you stand

```console
$ proton account get
Email:       you@proton.me
Name:        Roman
Storage:     ━━━━━───────────────   26%  128.4 GB of 500.0 GB
Max Upload:  5.0 GB
Profile:     default
Session:     valid
Unlocked:    yes
ID:          Kd91mQxT…
```

## Run your first commands

```bash
proton mail messages list --unread
proton mail messages get "Invoice #2291"
proton drive items list /Documents
proton calendar events list
proton pass items get github.com
proton contacts list
```

Every command reads the same way, `proton <app> <collection> <verb>`, and anywhere one wants an ID, a subject, name, path or address works too.

That grammar is the whole trick: [learn it once](commands.md) and you can guess the rest.

## Turn on completion

```bash
# zsh
proton completion zsh > "${fpath[1]}/_proton"

# bash
proton completion bash | sudo tee /etc/bash_completion.d/proton

# fish
proton completion fish > ~/.config/fish/completions/proton.fish
```

Completion covers every command and flag, and offers real values as you type: your folder names, item types, output formats and setting keys.

## Preview a change before making it

Every command that changes something takes `--dry-run`. It resolves references, applies filters, and shows you the rows it would touch:

```bash
proton mail messages trash --from newsletter@example.com --older-than 90d --dry-run
```

Anything that removes permanently, or removes what a filter picked out rather than what you named, shows those rows and asks first. See [Dry runs and confirmations](using/confirmations.md).

## Sign out

```bash
proton account logout             # forget the session on this machine
proton account logout --revoke    # and invalidate it at Proton
```

Revoking also makes the credentials saved on this machine useless, even to someone who already copied them.

## Where to go next

| Page | What it covers |
| --- | --- |
| [How a command is built](commands.md) | The grammar, and the verbs |
| [Mail](mail/README.md) · [Drive](drive/README.md) · [Calendar](calendar/README.md) · [Pass](pass/README.md) · [Contacts](contacts/README.md) | One guide per app, task by task |
| [Account](account/README.md) | Two accounts side by side, unattended sign-in, revoking a device |
| [Scripting](using/scripting.md) | Pipelines, `jq`, cron and systemd |
| [All commands](about/commands.md) | Every command in one table |
| [FAQ](about/faq.md) | Is this official? How does it differ from Bridge? Is my password safe? |
