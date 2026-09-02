# proton pass items

Logins, notes, cards and the rest.

Every command under `proton pass items`, with the arguments and flags it takes. For these commands in use, see [the pass guide](README.md).

Holds `create`, `delete`, `get`, `list`, `move`, `pin`, `revisions`, `share`, `totp`, `trash`, `unpin` and `update`.

## `create`

Create an item.

A secret is read from a file or from stdin, never from a flag value: --secret-file NAME=FILE, or --secret-stdin NAME for one of them. NAME is cvv, number, password, pin, private-key, totp-uri, or any name at all, which makes a hidden custom field of it.

--generate-password makes one instead, so a new login needs no file: it is shaped by the same flags `pass generate` takes.

```
proton pass items create
```

```bash
proton pass items create --name GitHub --username roman --url github.com --generate-password
proton pass items create --name Router --generate-password --words 5
proton pass items create --type note --name 'Door codes' --note 'Front: 1234'
proton pass items create --type credit-card --name 'Travel card' --holder 'Roman' --expiry 2030-04 --secret-file number=/run/secrets/card
proton pass items create --type custom --name Router --field 'Network/SSID=home' --secret-file 'Network/Key=/run/secrets/wifi'
```

| Flag | Description |
| --- | --- |
| `--address string` | Set the address (identity) |
| `--birthdate string` | Set the birthdate (identity) |
| `--city string` | Set the city (identity) |
| `--company string` | Set the company (identity) |
| `--country string` | Set the country (identity) |
| `--county string` | Set the county (identity) |
| `--email string` | Set the email address (login) |
| `--expiry string` | Set the card expiry, YYYY-MM (credit-card) |
| `--facebook string` | Set the facebook (identity) |
| `--field stringArray` | Set a custom field, as NAME=VALUE or SECTION/NAME=VALUE (repeatable) |
| `--first-name string` | Set the first name (identity) |
| `--floor string` | Set the floor (identity) |
| `--full-name string` | Set the full name (identity) |
| `--gender string` | Set the gender (identity) |
| `--generate-password` | Make the password rather than being given one |
| `--holder string` | Set the cardholder's name (credit-card) |
| `--instagram string` | Set the instagram (identity) |
| `--job-title string` | Set the job title (identity) |
| `--last-name string` | Set the last name (identity) |
| `--length int` | How many characters (default `20`) |
| `--license-number string` | Set the license number (identity) |
| `--linkedin string` | Set the linkedin (identity) |
| `--middle-name string` | Set the middle name (identity) |
| `--name string` | Set the item's name |
| `--no-digits` | Leave the digits out |
| `--no-symbols` | Leave the symbols out |
| `--no-uppercase` | Leave the capitals out |
| `--note string` | Set the note |
| `--organization string` | Set the organization (identity) |
| `--passport-number string` | Set the passport number (identity) |
| `--personal-website string` | Set the personal website (identity) |
| `--phone string` | Set the phone (identity) |
| `--postal-code string` | Set the postal code (identity) |
| `--public-key string` | Set the public key (ssh-key) |
| `--reddit string` | Set the reddit (identity) |
| `--second-phone string` | Set the second phone (identity) |
| `--secret-file stringArray` | Read a secret field from a file, as NAME=FILE (repeatable) |
| `--secret-stdin string` | Read the named secret field from stdin |
| `--security string` | Wi-Fi security (wifi): WPA, WPA2, WPA3, WEP |
| `--separator string` | What stands between the words of a passphrase: comma, digit, hyphen, period, space, symbol, underscore (default `hyphen`) |
| `--social-security-number string` | Set the social security number (identity) |
| `--ssid string` | Set the network name (wifi) |
| `--state string` | Set the state (identity) |
| `--type string` | What kind of item: login, note, credit-card, wifi, ssh-key, identity, alias, custom (default `login`) |
| `--url string` | Set the URL (login) |
| `--username string` | Set the username (login) |
| `--vault string` | Which vault, by name or ID (default: your first) |
| `--website string` | Set the website (identity) |
| `--words int` | Make a passphrase of this many words instead |
| `--work-email string` | Set the work email (identity) |
| `--work-phone string` | Set the work phone (identity) |
| `--x-handle string` | Set the x handle (identity) |
| `--yahoo string` | Set the yahoo (identity) |

## `delete`

Delete items permanently.

```
proton pass items delete [REF...]
```

```bash
proton pass items delete GitHub
proton pass items delete --vault Work --all --yes
```

| Flag | Description |
| --- | --- |
| `--all` | Act on everything in scope, rather than a subset |
| `--newer-than string` | Match items newer than DURATION |
| `--older-than string` | Match items older than DURATION (e.g. 30d, 2w, 1h) |
| `--type string` | Match only this kind of item: login, note, credit-card, wifi, ssh-key, identity, alias, custom |
| `--vault string` | Match only this vault, by name or ID |

## `get`

Show one item, decrypted.

Passwords, TOTP secrets and private keys are printed in full. This is the only command that prints them; the listings do not.

```
proton pass items get REF
```

```bash
proton pass items get github.com
proton pass items get GitHub --output json
```

## `list`

List items across your vaults.

Takes the same filters as trash and delete, so you can preview a selection here before acting on it.

```
proton pass items list
```

```bash
proton pass items list
proton pass items list --vault Work
proton pass items list --type login
```

| Flag | Description |
| --- | --- |
| `--desc` | Reverse the order |
| `--newer-than string` | Match items newer than DURATION |
| `--older-than string` | Match items older than DURATION (e.g. 30d, 2w, 1h) |
| `--page int` | Which page of results, counting from zero |
| `--page-size int` | How many items per page (default `50`) |
| `--sort string` | Order by: name, type, modified, created (default `name`) |
| `--type string` | Match only this kind of item: login, note, credit-card, wifi, ssh-key, identity, alias, custom |
| `--vault string` | Match only this vault, by name or ID |

## `move`

Put an item in another vault.

The item keeps its history and everything it holds, but gets a new ID: an item in Pass is identified by its vault as well as itself. The new ID is printed on stdout.

```
proton pass items move REF
```

```bash
proton pass items move github.com --into Work
```

| Flag | Description |
| --- | --- |
| `--into string` | Which vault to put it in, by name or ID |

## `pin`

Keep items at the top of the list.

```
proton pass items pin REF...
```

```bash
proton pass items pin github.com
```

## `revisions`

Earlier versions of an item.

Holds `get` and `list`.

### `revisions get`

Show one earlier version, decrypted.

The password, TOTP secret and private key that revision held are printed in full, as `items get` prints the current ones.

REVISION_REF is the number `revisions list` shows.

```
proton pass items revisions get REF REVISION_REF
```

```bash
proton pass items revisions get github.com 3
```

### `revisions list`

Show what an item used to be.

Pass keeps every edit, so a password changed by mistake can be recovered. Newest first.

This says what changed and when. To read one revision back in full, use `revisions get`.

```
proton pass items revisions list REF
```

```bash
proton pass items revisions list github.com
proton pass items revisions list github.com --output json
```

## `share`

Who else can open an item.

Holds `add`, `get`, `remove` and `update`.

### `share add`

Offer one item to somebody, leaving the vault around it alone.

What travels is the item's own key rather than the vault's, so they can open that item and nothing else sealed under the same share.

```
proton pass items share add REF EMAIL
```

```bash
proton pass items share add github.com jane@proton.me
proton pass items share add github.com jane@proton.me --access editor
```

| Flag | Description |
| --- | --- |
| `--access string` | What they may do with it: viewer, editor, manager (default `viewer`) |

### `share get`

Show how an item is shared: who holds it, who has been offered it, and the links made for it.

A link's URL carries the key that opens the item, so this prints it in full - as `links get` does, and as a listing never does.

```
proton pass items share get REF
```

```bash
proton pass items share get github.com
```

### `share remove`

Take somebody's access to an item away.

It withdraws an invitation nobody answered, or removes a member who did.

```
proton pass items share remove REF EMAIL
```

```bash
proton pass items share remove github.com jane@proton.me
```

### `share update`

Change what somebody may do with an item.

A member's access changes in place. Somebody who has not answered yet has their offer withdrawn and made again at the new access, which sends them a fresh invitation.

```
proton pass items share update REF EMAIL
```

```bash
proton pass items share update github.com jane@proton.me --access viewer
```

| Flag | Description |
| --- | --- |
| `--access string` | What they may do with it: viewer, editor, manager (default `viewer`) |

## `totp`

Print the current two-factor code for an item.

How long the code has left is reported beside it, so you can tell whether to wait for the next one.

For a script: --output json, then read .code.

```
proton pass items totp REF
```

```bash
proton pass items totp github.com
proton pass items totp github.com --output json
```

## `trash`

Move items to the trash.

```
proton pass items trash [REF...]
```

```bash
proton pass items trash GitHub
proton pass items trash --vault Work --older-than 1y
```

| Flag | Description |
| --- | --- |
| `--all` | Act on everything in scope, rather than a subset |
| `--newer-than string` | Match items newer than DURATION |
| `--older-than string` | Match items older than DURATION (e.g. 30d, 2w, 1h) |
| `--type string` | Match only this kind of item: login, note, credit-card, wifi, ssh-key, identity, alias, custom |
| `--vault string` | Match only this vault, by name or ID |

## `unpin`

Stop keeping items at the top.

```
proton pass items unpin REF...
```

```bash
proton pass items unpin github.com
```

## `update`

Change an item's fields.

A secret is read from a file or from stdin, never from a flag value: --secret-file NAME=FILE, or --secret-stdin NAME for one of them. NAME is cvv, number, password, pin, private-key, totp-uri, or any name at all, which makes a hidden custom field of it.

--generate-password replaces the password with one it makes.

```
proton pass items update REF
```

```bash
proton pass items update GitHub --secret-file password=/run/secrets/github
proton pass items update GitHub --secret-stdin password
proton pass items update GitHub --username roman-16 --url github.com
proton pass items update GitHub --generate-password
```

| Flag | Description |
| --- | --- |
| `--address string` | Replace the address (identity) |
| `--birthdate string` | Replace the birthdate (identity) |
| `--city string` | Replace the city (identity) |
| `--company string` | Replace the company (identity) |
| `--country string` | Replace the country (identity) |
| `--county string` | Replace the county (identity) |
| `--display-name string` | Replace the name recipients see on mail from it (alias) |
| `--email string` | Replace the email address (login) |
| `--expiry string` | Replace the card expiry, YYYY-MM (credit-card) |
| `--facebook string` | Replace the facebook (identity) |
| `--field stringArray` | Replace a custom field, as NAME=VALUE or SECTION/NAME=VALUE (repeatable) |
| `--first-name string` | Replace the first name (identity) |
| `--floor string` | Replace the floor (identity) |
| `--full-name string` | Replace the full name (identity) |
| `--gender string` | Replace the gender (identity) |
| `--generate-password` | Make the password rather than being given one |
| `--holder string` | Replace the cardholder's name (credit-card) |
| `--instagram string` | Replace the instagram (identity) |
| `--job-title string` | Replace the job title (identity) |
| `--last-name string` | Replace the last name (identity) |
| `--length int` | How many characters (default `20`) |
| `--license-number string` | Replace the license number (identity) |
| `--linkedin string` | Replace the linkedin (identity) |
| `--mailbox stringArray` | Replace where mail to it arrives (alias, repeatable) |
| `--middle-name string` | Replace the middle name (identity) |
| `--name string` | Replace the item's name |
| `--no-digits` | Leave the digits out |
| `--no-symbols` | Leave the symbols out |
| `--no-uppercase` | Leave the capitals out |
| `--note string` | Replace the note |
| `--organization string` | Replace the organization (identity) |
| `--passport-number string` | Replace the passport number (identity) |
| `--personal-website string` | Replace the personal website (identity) |
| `--phone string` | Replace the phone (identity) |
| `--postal-code string` | Replace the postal code (identity) |
| `--public-key string` | Replace the public key (ssh-key) |
| `--reddit string` | Replace the reddit (identity) |
| `--second-phone string` | Replace the second phone (identity) |
| `--secret-file stringArray` | Read a secret field from a file, as NAME=FILE (repeatable) |
| `--secret-stdin string` | Read the named secret field from stdin |
| `--security string` | Wi-Fi security (wifi): WPA, WPA2, WPA3, WEP |
| `--separator string` | What stands between the words of a passphrase: comma, digit, hyphen, period, space, symbol, underscore (default `hyphen`) |
| `--social-security-number string` | Replace the social security number (identity) |
| `--ssid string` | Replace the network name (wifi) |
| `--state string` | Replace the state (identity) |
| `--url string` | Replace the URL (login) |
| `--username string` | Replace the username (login) |
| `--website string` | Replace the website (identity) |
| `--words int` | Make a passphrase of this many words instead |
| `--work-email string` | Replace the work email (identity) |
| `--work-phone string` | Replace the work phone (identity) |
| `--x-handle string` | Replace the x handle (identity) |
| `--yahoo string` | Replace the yahoo (identity) |

---

Every command also takes the [flags that work everywhere](../about/commands.md#flags-that-work-on-every-command).
