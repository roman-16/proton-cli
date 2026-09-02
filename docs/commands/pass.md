# proton pass

Vaults, logins and secrets.

Every command under `proton pass`, with the arguments and flags it takes. For these commands in use, see [the guide](../apps/pass.md).

Holds `aliases`, `breaches`, `export`, `generate`, `import`, `invitations`, `items`, `links`, `settings`, `shared`, `sharing`, `trash` and `vaults`.

## `aliases`

Hide-my-email addresses that forward to you.

Holds `contacts`, `create`, `disable`, `enable`, `list` and `options`.

### `aliases contacts`

Addresses an alias can write to.

Holds `allow`, `block`, `create`, `delete` and `list`.

### `aliases contacts allow`

Let a contact's mail reach you again.

```
proton pass aliases contacts allow REF CONTACT_REF...
```

```bash
proton pass aliases contacts allow shopping seller@example.com
```

### `aliases contacts block`

Stop a contact's mail reaching you.

```
proton pass aliases contacts block REF CONTACT_REF...
```

```bash
proton pass aliases contacts block shopping seller@example.com
```

### `aliases contacts create`

Make an address that writes to somebody as the alias.

Proton answers with a second address standing for that one person. Mail you send there reaches them as though the alias had written it, so a reply never shows the address behind it.

```
proton pass aliases contacts create REF EMAIL
```

```bash
proton pass aliases contacts create shopping seller@example.com
proton pass aliases contacts create shopping seller@example.com --name "The seller"
```

| Flag | Description |
| --- | --- |
| `--name string` | A name for them |

### `aliases contacts delete`

Remove an address an alias can write to.

```
proton pass aliases contacts delete REF CONTACT_REF...
```

```bash
proton pass aliases contacts delete shopping seller@example.com
```

### `aliases contacts list`

List the addresses an alias can write to.

```
proton pass aliases contacts list REF
```

```bash
proton pass aliases contacts list shopping
```

### `aliases create`

Create an alias.

The address is a prefix you choose plus a suffix Proton offers; mail sent to it arrives in the mailboxes you name. `aliases options` lists both.

```
proton pass aliases create
```

```bash
proton pass aliases create --prefix shop --mailbox me@proton.me
proton pass aliases create --prefix news --mailbox me@proton.me --vault Work --name 'Newsletter alias'
```

| Flag | Description |
| --- | --- |
| `--mailbox stringArray` | Where mail to the alias should arrive (repeatable) |
| `--name string` | Name for the alias item |
| `--prefix string` | The part before the @ |
| `--suffix string` | The part from the @ onwards (default: the first Proton offers) |
| `--vault string` | Which vault to keep it in, by name or ID |

### `aliases disable`

Stop receiving mail sent to an alias.

```
proton pass aliases disable REF
```

```bash
proton pass aliases disable shop
```

### `aliases enable`

Start receiving mail sent to an alias.

```
proton pass aliases enable REF
```

```bash
proton pass aliases enable shop
```

### `aliases list`

List your aliases.

```
proton pass aliases list
```

```bash
proton pass aliases list
proton pass aliases list --vault Work
```

| Flag | Description |
| --- | --- |
| `--vault string` | Show only this vault, by name or ID |

### `aliases options`

List the suffixes and mailboxes an alias can use.

A suffix is the domain an address is made on, and what --suffix takes. Proton puts a word of its own in front of it and only settles on one when the alias is created.

```
proton pass aliases options
```

```bash
proton pass aliases options
```

## `breaches`

Addresses that have appeared in a data breach.

Holds `get` and `list`.

### `breaches get`

Show the breaches one address has appeared in.

```
proton pass breaches get REF
```

```bash
proton pass breaches get jane@proton.me
```

### `breaches list`

List the addresses Proton watches, and how many breaches each is in.

Worst first, because the reason to run this is to find what to deal with. `breaches get` on one of them says which breaches, and what they exposed.

```
proton pass breaches list
```

```bash
proton pass breaches list
```

## `export`

Write the vaults you own out as a Proton Pass archive, or to stdout with --dest -.

The file is the one Proton Pass itself writes, so it can be read back by the app as well as by this tool. Give a passphrase and the contents are encrypted to it; without one the archive holds every password in the clear.

A vault somebody shared with you is theirs to back up, so it is not in the file - restoring this one cannot turn their vault into a second copy under your name.

```
proton pass export
```

```bash
proton pass export --dest pass-backup.zip --passphrase-file ~/.backup-passphrase
proton pass export --dest pass-backup.zip
```

| Flag | Description |
| --- | --- |
| `--dest string` | Write to this path, or - for stdout |
| `--dest-dir string` | Write into this directory, keeping each item's own name |
| `--force` | Overwrite a file that already exists |
| `--passphrase-file string` | Read the passphrase that locks the file from a file |
| `--passphrase-stdin` | Read the passphrase that locks the file from stdin |

## `generate`

Make a password, without storing it anywhere.

It reaches no account and needs no session. The alphabet is Proton's own, which leaves out i, o, l and their capitals - the characters people misread - unless letters are all the password has.

Every kind asked for is guaranteed to appear, so a password that has to contain a digit does.

--words makes a passphrase instead, from Proton's own wordlist: that many words, capitalised and each followed by a digit unless --no-uppercase or --no-digits says otherwise.

```
proton pass generate
```

```bash
proton pass generate
proton pass generate --length 32
proton pass generate --no-symbols --length 24
proton pass generate --words 4
proton pass generate --words 4 --separator space --no-digits
```

| Flag | Description |
| --- | --- |
| `--length int` | How many characters (default `20`) |
| `--no-digits` | Leave the digits out |
| `--no-symbols` | Leave the symbols out |
| `--no-uppercase` | Leave the capitals out |
| `--separator string` | What stands between the words of a passphrase: comma, digit, hyphen, period, space, symbol, underscore (default `hyphen`) |
| `--words int` | Make a passphrase of this many words instead |

## `import`

Read a Proton Pass archive back in, or one on stdin with -.

A vault in the file lands in the vault of that name, and one that is not there yet is made. Items are added rather than matched: nothing in a file says which existing item it was, so reading the same file twice puts the items in twice.

```
proton pass import PATH
```

```bash
proton pass import pass-backup.zip --passphrase-file ~/.backup-passphrase
proton pass import --dry-run pass-backup.zip
```

| Flag | Description |
| --- | --- |
| `--passphrase-file string` | Read the passphrase that locks the file from a file |
| `--passphrase-stdin` | Read the passphrase that locks the file from stdin |

## `invitations`

What other people have offered you.

Holds `accept`, `decline` and `list`.

### `invitations accept`

Take what somebody offered you.

The keys arrive encrypted to the address the offer was sent to and are moved onto your own key, which is what makes a vault open like any other of yours afterwards. An item taken this way is in no vault of yours, so it is `shared list` that has it.

```
proton pass invitations accept REF...
```

```bash
proton pass invitations accept Work
```

### `invitations decline`

Turn down what somebody offered you.

```
proton pass invitations decline REF...
```

```bash
proton pass invitations decline Work
```

### `invitations list`

List what other people have offered you.

A vault's name and how much is in it are readable before you take it: the invitation carries the key that opens them, encrypted to you. What is in the vault is not, until you accept. An item offered on its own carries no preview at all.

```
proton pass invitations list
```

```bash
proton pass invitations list
```

## `items`

Logins, notes, cards and the rest.

Holds `create`, `delete`, `get`, `list`, `move`, `pin`, `revisions`, `share`, `totp`, `trash`, `unpin` and `update`.

### `items create`

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

### `items delete`

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

### `items get`

Show one item, decrypted.

Passwords, TOTP secrets and private keys are printed in full: this is the command for reading a secret, so it does not hide one.

```
proton pass items get REF
```

```bash
proton pass items get github.com
proton pass items get GitHub --output json
```

### `items list`

List items across your vaults.

The filters are the same ones trash and delete take, so a selection can be worked out here before it is handed to a verb that acts on it.

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

### `items move`

Put an item in another vault.

The item keeps its history and everything it holds, and is given a new ID: an item in Pass is only unique together with the vault it is in. The new ID is printed, so a script can go on addressing it.

```
proton pass items move REF
```

```bash
proton pass items move github.com --into Work
```

| Flag | Description |
| --- | --- |
| `--into string` | Which vault to put it in, by name or ID |

### `items pin`

Keep items at the top of the list.

```
proton pass items pin REF...
```

```bash
proton pass items pin github.com
```

### `items revisions`

Earlier versions of an item.

Holds `get` and `list`.

### `items revisions get`

Show one earlier version, decrypted.

The password, TOTP secret and private key that revision held are printed in full, as `items get` prints the current ones: this is the command for reading a password an item used to have.

REVISION_REF is the number `revisions list` shows.

```
proton pass items revisions get REF REVISION_REF
```

```bash
proton pass items revisions get github.com 3
```

### `items revisions list`

Show what an item used to be.

Pass keeps every edit, so a password changed by mistake can be found again. Newest first. This is what changed and when; `revisions get` reads one of them back in full.

```
proton pass items revisions list REF
```

```bash
proton pass items revisions list github.com
proton pass items revisions list github.com --output json
```

### `items share`

Who else can open an item.

Holds `add`, `get`, `remove` and `update`.

### `items share add`

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

### `items share get`

Show how an item is shared: who holds it, who has been offered it, and the links made for it.

A link's URL carries the key that opens the item, so this prints it in full - as `links get` does, and as a listing never does.

```
proton pass items share get REF
```

```bash
proton pass items share get github.com
```

### `items share remove`

Take somebody's access to an item away.

It withdraws an invitation nobody answered, or removes a member who did.

```
proton pass items share remove REF EMAIL
```

```bash
proton pass items share remove github.com jane@proton.me
```

### `items share update`

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

### `items totp`

Print the current two-factor code for an item.

How long the code has left is reported beside it, because a code about to expire is one worth waiting out.

For a script: --output json, then read .code.

```
proton pass items totp REF
```

```bash
proton pass items totp github.com
proton pass items totp github.com --output json
```

### `items trash`

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

### `items unpin`

Stop keeping items at the top.

```
proton pass items unpin REF...
```

```bash
proton pass items unpin github.com
```

### `items update`

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

## `links`

Links that show an item to somebody without an account.

Holds `create`, `get`, `list` and `revoke`.

### `links create`

Make a link that shows one item to somebody with no Proton account.

The key that opens it travels in the URL after the '#', which a browser never sends to Proton. So the URL is the secret: anyone holding the whole of it can read the item until the link expires or is revoked.

--expires is required.

```
proton pass links create REF
```

```bash
proton pass links create github.com --expires 7d
proton pass links create github.com --expires 24h --views 1
```

| Flag | Description |
| --- | --- |
| `--expires string` | How long the link lasts (e.g. 7d, 24h) |
| `--views int` | Stop working after this many openings |

### `links get`

Show one link, URL and all.

Proton stores the key sealed under the item's own, so a link you mislaid is read back here rather than revoked and made again. The URL is the secret, which is why it takes a command that says so rather than appearing in a listing.

```
proton pass links get REF
```

```bash
proton pass links get 5bH2mQxK
```

### `links list`

List the links you have made.

The URL is not among them: it carries the key that opens the item, and a listing is no place for a secret. `links get` reads one back whole, and `items share get` the ones an item has.

```
proton pass links list
```

```bash
proton pass links list
```

### `links revoke`

Stop a link working.

The item is untouched; only the link is withdrawn. Anyone who already read it has already read it.

```
proton pass links revoke REF...
```

```bash
proton pass links revoke 5bH2mQxK
```

## `settings`

Pass settings.

Holds `domains` and `mailboxes`.

### `settings domains`

The domains an alias can be made on.

Holds `list`.

### `settings domains list`

List the domains an alias can be made on.

These are the part after the @ that `proton pass aliases create --suffix` chooses between.

```
proton pass settings domains list
```

```bash
proton pass settings domains list
```

### `settings mailboxes`

The addresses your aliases forward to.

Holds `create`, `delete`, `list`, `resend`, `update` and `verify`.

### `settings mailboxes create`

Add an address for aliases to forward to.

Proton emails the address a code, and it receives nothing until the code is handed back with `mailboxes verify`.

```
proton pass settings mailboxes create EMAIL
```

```bash
proton pass settings mailboxes create me@example.com
```

### `settings mailboxes delete`

Remove an address aliases forward to.

Aliases arriving in it have to go somewhere: --transfer-to names the mailbox they move to. Without it they stop receiving, which is why it is asked for rather than assumed.

```
proton pass settings mailboxes delete REF
```

```bash
proton pass settings mailboxes delete me@example.com --transfer-to other@example.com
```

| Flag | Description |
| --- | --- |
| `--transfer-to string` | Move the aliases arriving here to this mailbox |

### `settings mailboxes list`

List the addresses your aliases forward to.

An alias is a route rather than a mailbox of its own: mail sent to it arrives in one of these. `proton pass items update REF --mailbox` is what points an alias at one.

```
proton pass settings mailboxes list
```

```bash
proton pass settings mailboxes list
```

### `settings mailboxes resend`

Send the confirmation code again.

```
proton pass settings mailboxes resend REF
```

```bash
proton pass settings mailboxes resend me@example.com
```

### `settings mailboxes update`

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

### `settings mailboxes verify`

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

## `shared`

Items other people have shared with you.

Holds `list`.

### `shared list`

List the items other people have shared with you.

These are in no vault of yours, so they are not in `items list`: they are addressed by the ID this shows, or by their name like anything else.

An item whose content will not open is still listed, because knowing it is there is what lets you act on it.

```
proton pass shared list
```

```bash
proton pass shared list
```

## `sharing`

Items you have shared with other people.

Holds `list`.

### `sharing list`

List the items you have shared with somebody on their own.

`items share get REF` answers the question for one item; this answers the one you actually have, which is what have I left open. A vault you share is in `vaults list`, with the number of people in it, and a link you made is in `links list`.

```
proton pass sharing list
```

```bash
proton pass sharing list
```

## `trash`

Items you have removed but not yet deleted.

Holds `empty`, `list` and `restore`.

### `trash empty`

Delete everything in the trash, permanently.

```
proton pass trash empty
```

```bash
proton pass trash empty
```

### `trash list`

List what is in the trash.

```
proton pass trash list
```

```bash
proton pass trash list
```

### `trash restore`

Put items back where they came from.

```
proton pass trash restore [REF...]
```

```bash
proton pass trash restore GitHub
proton pass trash restore --all
```

| Flag | Description |
| --- | --- |
| `--all` | Act on everything in scope, rather than a subset |

## `vaults`

The vaults your items live in.

Holds `create`, `delete`, `get`, `list`, `share`, `transfer` and `update`.

### `vaults create`

Create a vault.

```
proton pass vaults create
```

```bash
proton pass vaults create --name Work
```

| Flag | Description |
| --- | --- |
| `--name string` | Name for the new vault |

### `vaults delete`

Delete vaults, and everything in them.

```
proton pass vaults delete REF...
```

```bash
proton pass vaults delete Work
```

### `vaults get`

Show one vault in full.

```
proton pass vaults get REF
```

```bash
proton pass vaults get Work
```

### `vaults list`

List your vaults.

```
proton pass vaults list
```

```bash
proton pass vaults list
```

### `vaults share`

Who else can open a vault.

Holds `add`, `get`, `remove` and `update`.

### `vaults share add`

Offer a vault to somebody.

They are sent an invitation and see nothing until they take it. What is sent is the key that opens the vault, encrypted to their key and signed with yours - so it has to be another Proton account, because an address Proton holds no keys for has nothing to encrypt to.

```
proton pass vaults share add REF EMAIL
```

```bash
proton pass vaults share add Work jane@proton.me
proton pass vaults share add Work jane@proton.me --access editor
```

| Flag | Description |
| --- | --- |
| `--access string` | What they may do with it: viewer, editor, manager (default `viewer`) |

### `vaults share get`

Show who can open a vault.

Members have accepted; the invited have not answered yet.

```
proton pass vaults share get REF
```

```bash
proton pass vaults share get Work
```

### `vaults share remove`

Take somebody's access to a vault away.

It withdraws an invitation nobody answered, or removes a member who did. The vault is untouched; anything they already read they have read.

```
proton pass vaults share remove REF EMAIL
```

```bash
proton pass vaults share remove Work jane@proton.me
```

### `vaults share update`

Change what somebody may do with a vault.

For a member, nothing is re-encrypted: the key they hold still opens the vault, and only what they may do with it changes. Somebody who has not answered yet holds nothing to change, so the offer is withdrawn and made again at the new access - which sends them a fresh invitation.

```
proton pass vaults share update REF EMAIL
```

```bash
proton pass vaults share update Work jane@proton.me --access manager
```

| Flag | Description |
| --- | --- |
| `--access string` | What they may do with it: viewer, editor, manager (default `viewer`) |

### `vaults transfer`

Make somebody else the owner of a vault.

They have to be a member already, and only the owner can hand a vault over. Afterwards you are a manager of it like anybody else, so this is the one change to a vault you cannot undo on your own.

```
proton pass vaults transfer REF EMAIL
```

```bash
proton pass vaults transfer Work jane@proton.me
```

### `vaults update`

Rename a vault, or change how it looks.

Pass shows its icons and colors as a grid with no names, so the numbers are what there is: --icon 7, --color 3. Anything not mentioned is left alone, including a description written in the Pass app.

```
proton pass vaults update REF
```

```bash
proton pass vaults update Work --name Office
proton pass vaults update Work --description 'Shared team logins' --icon 7 --color 3
```

| Flag | Description |
| --- | --- |
| `--color string` | Which of Pass's vault colors it takes: 1, 2, 3, 4, 5, 6, 7, 8, 9, 10 |
| `--description string` | What the vault is for |
| `--icon string` | Which of Pass's icons represents it: 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30 |
| `--name string` | New name |

---

Every command also takes the [flags that work everywhere](README.md#flags-that-work-on-every-command).
