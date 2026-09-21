# proton pass settings

Pass settings.

Every command under `proton pass settings`, with the arguments and flags it takes. For these commands in use, see [the pass guide](README.md).

Holds `access-tokens`, `domains`, `extra-password` and `mailboxes`.

## `access-tokens`

Tokens that let a program into Pass without your password.

A token is for Proton's pass-cli, or for an AI agent working through it. It reads the vaults you hand it and nothing else, and it stops working when it expires. Making one needs a paid Pass plan.

The token itself is shown once, when it is made.

Holds `activity`, `create`, `delete`, `get`, `list` and `update`.

### `access-tokens activity`

What a token has been used for.

Holds `list`.

### `access-tokens activity list`

List what a token has done, newest first.

Each action is Proton's record of it. The item, vault and reason are what the program wrote down, which a token made with `access-tokens create --agent` has to do for every action; one made without has actions and no reasons.

```
proton pass settings access-tokens activity list REF
```

```bash
proton pass settings access-tokens activity list agent
proton pass settings access-tokens activity list agent --limit 20
```

| Flag | Description |
| --- | --- |
| `--limit int` | How many actions per page; 0 for all of them |
| `--page int` | Which page of results, counting from zero |

### `access-tokens create`

Make a token for a program to use.

--name, --expires and at least one --vault are required. The token reads the vaults named and nothing else, and stops working when --expires runs out, which is between 1h and 1y from now.

--agent marks it for an AI agent, which then has to give a reason for every action it takes; `activity list` shows them.

The token is shown once and never again. Under --output json it is the `secret` field.

```
proton pass settings access-tokens create
```

```bash
proton pass settings access-tokens create --name ci --expires 30d --vault Work
proton pass settings access-tokens create --name agent --expires 1d --vault Work --agent
proton pass settings access-tokens create --name ci --expires 30d --vault Work --output json
```

| Flag | Description |
| --- | --- |
| `--agent` | Make it for an AI agent, which has to give a reason for every action |
| `--expires string` | How long the token works (e.g. 30d, 12h); 1h to 1y |
| `--name string` | Name for the new token |
| `--vault stringArray` | A vault the token may read, by name or ID (repeatable) |

### `access-tokens delete`

Stop a token working, for good.

A program holding the token is refused from then on. Deleting one that has already expired only takes it off the list.

```
proton pass settings access-tokens delete REF...
```

```bash
proton pass settings access-tokens delete ci
```

### `access-tokens get`

Show a token and the vaults it reads.

The token itself is not here: it was shown once, when it was made, and nothing brings it back. A token you have lost is deleted and made again.

```
proton pass settings access-tokens get REF
```

```bash
proton pass settings access-tokens get ci
```

### `access-tokens list`

List the tokens, expired ones included.

STATUS is active, expiring or expired; expiring means within the hour. A token that expired more than thirty days ago is gone from the list. The tokens themselves are never shown here.

```
proton pass settings access-tokens list
```

```bash
proton pass settings access-tokens list
proton pass settings access-tokens list --output json
```

| Flag | Description |
| --- | --- |
| `--desc` | Reverse the order |
| `--limit int` | How many access tokens per page; 0 for all of them |
| `--page int` | Which page of results, counting from zero |
| `--sort string` | Order by: created, name, expires (default `created`) |

### `access-tokens update`

Change which vaults a token reads.

--vault names the whole set, one flag per vault: a vault not named is taken away, and one the token did not have is handed to it. An expired token cannot be changed; delete it and make another.

```
proton pass settings access-tokens update REF
```

```bash
proton pass settings access-tokens update ci --vault Work --vault Personal
```

| Flag | Description |
| --- | --- |
| `--vault stringArray` | A vault the token may read, by name or ID (repeatable) |

## `domains`

The domains an alias can be made on.

Proton's own are always there. A custom domain is one you own and needs a paid Pass plan; it carries no alias until its DNS entries are in place. `get` shows them and says which ones Proton can see.

Holds `create`, `delete`, `get`, `list` and `update`.

### `domains create`

Add a domain of your own for aliases to be made on.

DOMAIN is a domain you own, written as example.com. Adding one needs a paid Pass plan.

The domain carries no alias until its DNS entries are in place: first the one that proves it is yours, then the MX entries that bring its mail here. `get` shows every entry the domain needs.

```
proton pass settings domains create DOMAIN
```

```bash
proton pass settings domains create example.com
```

### `domains delete`

Remove a domain of your own.

Every alias made on the domain is deleted with it, and an alias address cannot be brought back. Adding the domain again needs its DNS entries verified from scratch.

```
proton pass settings domains delete REF
```

```bash
proton pass settings domains delete example.com
```

### `domains get`

Show a domain, with the DNS entries a custom one needs.

Reads a custom domain's DNS again each time it runs, so what it reports is what Proton can see now. Each check reads ok, missing or wrong, and a wrong one says what was found instead. Ownership and MX are what a domain needs to carry aliases; SPF, DKIM and DMARC keep its mail out of spam.

The catch-all, display name and random prefix appear once the domain is verified.

```
proton pass settings domains get REF
```

```bash
proton pass settings domains get example.com
proton pass settings domains get example.com --output json
```

### `domains list`

List the domains an alias can be made on.

These are the values `proton pass aliases create --suffix` accepts: the part of an alias after the @. A custom domain is listed whatever state it is in; STATUS is active, unverified or unconfigured, and is blank for one of Proton's.

```
proton pass settings domains list
```

```bash
proton pass settings domains list
proton pass settings domains list --output json
```

| Flag | Description |
| --- | --- |
| `--desc` | Reverse the order |
| `--limit int` | How many domains per page; 0 for all of them |
| `--page int` | Which page of results, counting from zero |
| `--sort string` | Order by: domain (default `domain`) |

### `domains update`

Change a domain.

--default makes new aliases take this domain when nothing names another, and --default=false leaves none chosen. Every other flag is a custom domain's, and waits until the domain is verified.

--catch-all names the mailboxes that take mail sent to any name at the domain; an alias is made for the name as the first mail arrives. One flag per mailbox, and `--catch-all none` turns it off.

```
proton pass settings domains update REF
```

```bash
proton pass settings domains update example.com --default
proton pass settings domains update example.com --catch-all me@proton.me
proton pass settings domains update example.com --catch-all none
proton pass settings domains update example.com --display-name "Jane Roe" --random-prefix
```

| Flag | Description |
| --- | --- |
| `--catch-all stringArray` | Mailbox that takes mail sent to any name at the domain (repeatable), or none |
| `--clear-display-name` | Remove the display name |
| `--default` | Make new aliases take this domain; --default=false chooses none |
| `--display-name string` | Name recipients see on mail from the domain's aliases |
| `--random-prefix` | Put a random word in front of a new alias on the domain |

## `extra-password`

The extra password Pass can be protected with.

Holds `disable`, `enable` and `get`.

### `extra-password disable`

Remove the extra password from Pass.

The password is asked for first, or comes from --extra-password-file, which takes - for stdin.

Pass then opens with your account password alone, on every device. This session goes on working.

```
proton pass settings extra-password disable
```

```bash
proton pass settings extra-password disable
proton pass settings extra-password disable --extra-password-file /run/secrets/proton-pass
```

| Flag | Description |
| --- | --- |
| `--extra-password-file string` | Read the Pass extra password from a file, or - for stdin |

### `extra-password enable`

Protect Pass with an extra password.

The password comes from --extra-password-file, which takes - for stdin, or from a prompt that asks for it twice. It needs at least eight characters.

Keep it safe: without it nothing opens Pass, on any device. Your other devices ask for it the next time they open Pass, and this session goes on working.

A Pass that already has one is refused. Turn it off and on again to change it.

```
proton pass settings extra-password enable
```

```bash
proton pass settings extra-password enable
proton pass settings extra-password enable --extra-password-file /run/secrets/proton-pass
```

| Flag | Description |
| --- | --- |
| `--extra-password-file string` | Read the Pass extra password from a file, or - for stdin |

### `extra-password get`

Show whether Pass has an extra password.

```
proton pass settings extra-password get
```

```bash
proton pass settings extra-password get
```

## `mailboxes`

The addresses your aliases forward to.

Holds `create`, `delete`, `list`, `resend`, `update` and `verify`.

### `mailboxes create`

Add an address for aliases to forward to.

Proton emails the address a code. The mailbox receives nothing until you pass that code to `mailboxes verify`.

```
proton pass settings mailboxes create EMAIL
```

```bash
proton pass settings mailboxes create me@example.com
```

### `mailboxes delete`

Remove an address aliases forward to.

--transfer-to names the mailbox that its aliases move to. It is required: without a new mailbox, those aliases would stop receiving mail.

```
proton pass settings mailboxes delete REF
```

```bash
proton pass settings mailboxes delete me@example.com --transfer-to other@example.com
```

| Flag | Description |
| --- | --- |
| `--transfer-to string` | Move the aliases arriving here to this mailbox |

### `mailboxes list`

List the addresses your aliases forward to.

An alias is a route, not a mailbox: mail sent to it arrives in one of these. To point an alias at one, run `proton pass items update REF --mailbox`.

```
proton pass settings mailboxes list
```

```bash
proton pass settings mailboxes list
```

| Flag | Description |
| --- | --- |
| `--desc` | Reverse the order |
| `--limit int` | How many mailboxes per page; 0 for all of them |
| `--page int` | Which page of results, counting from zero |
| `--sort string` | Order by: email (default `email`) |

### `mailboxes resend`

Send the confirmation code again.

```
proton pass settings mailboxes resend REF
```

```bash
proton pass settings mailboxes resend me@example.com
```

### `mailboxes update`

Change a mailbox.

```
proton pass settings mailboxes update REF
```

```bash
proton pass settings mailboxes update me@example.com --default
```

| Flag | Description |
| --- | --- |
| `--default` | Make new aliases arrive here |

### `mailboxes verify`

Confirm an address with the code Proton emailed it.

```
proton pass settings mailboxes verify REF
```

```bash
proton pass settings mailboxes verify me@example.com --code 123456
```

| Flag | Description |
| --- | --- |
| `--code string` | The code Proton emailed the address |

---

Every command also takes the [flags that work everywhere](../about/commands.md#flags-that-work-on-every-command).
