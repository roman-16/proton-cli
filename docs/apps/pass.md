# Proton Pass from the command line

Logins, notes, cards, SSH keys, identities, aliases and two-factor codes in Proton Pass. Items are decrypted on your machine with the vault and item keys.

Every command and flag is in the [`proton pass` reference](../commands/pass.md). This page is the things people actually do.

An item takes two IDs to address, written as one token: `SHARE_ID/ITEM_ID`. A name or URL works instead.

## Find and read

```bash
proton pass items list --vault Work
proton pass items get github.com                # by name or URL
proton pass items totp github.com               # the current two-factor code
proton pass generate --length 32                # a new password, made locally
proton pass generate --words 4                  # or a passphrase
```

`get` prints the item's fields, including the password and the TOTP **secret**, to stdout. Pass stores the secret rather than the code, so `totp` is what works the current code out, and it reports how long that code has left.

**A listing carries no secret.** `items list`, `aliases list`, `trash list` and `shared list` show what an item is and where, in every format including JSON; the password, the card, the keys and the hidden fields are what `items get` is for, and it says so in its own help.

`generate` reaches no account and needs no session. Its alphabet is Proton's own, which leaves out `i`, `o`, `l` and their capitals unless letters are all the password has. Every kind you ask for is guaranteed to appear, and a length too short to hold one of each is refused. `--words N` makes a passphrase from Proton's own wordlist instead - capitalised, each word followed by a digit, joined by `--separator`.

## Create and edit

Every type takes `--name`, and optionally `--vault`, `--note` and `--field NAME=VALUE`.

```bash
proton pass items create --name GitHub --username roman --url github.com --generate-password
proton pass items create --name Router --generate-password --words 5

proton pass items create --type note --name "Door codes" --note "Front: 1234"
proton pass items create --type credit-card --name Visa --holder "Roman L" --expiry 2028-12 \
  --secret-file number=/run/secrets/pan --secret-file cvv=/run/secrets/cvv
proton pass items create --type wifi --name Home --ssid MyNetwork --security WPA2 --secret-file password=/run/secrets/wifi
proton pass items create --type ssh-key --name laptop --public-key "$(cat ~/.ssh/id_ed25519.pub)" \
  --secret-file private-key=~/.ssh/id_ed25519
proton pass items create --type identity --name Me --full-name "Jane Roe" --email jane@example.com --city Vienna

proton pass items update github.com --secret-file password=/run/secrets/github
pass-store show github | proton pass items update github.com --secret-stdin password
```

Types are `login` (the default), `note`, `credit-card`, `wifi`, `ssh-key`, `identity`, `alias` and `custom`. Identity stores thirty-one fields; the reference lists them. `update` takes the same flags as `create` and leaves anything you don't pass alone.

**A secret is never a flag value.** `argv` is readable by every user on the machine through `ps` and it survives in shell history, so the parts of an item that are secret arrive from a file or from stdin, the way the account password does ([why](../design-notes.md#why-a-password-is-never-a-flag-value)). `--secret-file NAME=FILE` can be given as often as you like; `--secret-stdin NAME` reads one of them from the stream, and only one thing per run may.

`NAME` is `password`, `totp-uri`, `number`, `cvv`, `pin` or `private-key` - the item's own secret fields - or any other name, which makes a hidden custom field of it. A value that is an `otpauth://` URI is stored as a two-factor field rather than a hidden one, because that is what such a value is.

**`--generate-password` means the common case needs no file at all**: the password is made on your machine, stored, and printed beside the new item's ID rather than into it. It takes the same shaping flags as `proton pass generate`, so `--words 5` stores a passphrase.

**Sections.** A field can name the heading it sits under, in the same token:

```bash
proton pass items create --type custom --name Router \
  --field "Network/SSID=home" --secret-file "Network/Key=/run/secrets/wifi" \
  --field "Admin/URL=http://192.168.0.1" --secret-file "Admin/Password=/run/secrets/router"
```

A field is identified by its section and name together, so `Network/Password` and `Admin/Password` are two fields. Only the types whose Pass editor offers headings can carry them: `custom`, `ssh-key`, `wifi` and `identity`.

## Move it

```bash
proton pass items move github.com --into Work
```

The item keeps its history and everything it holds. It is given a new ID, because an item in Pass is only unique together with its vault - the new one is printed, so a script can go on addressing it.

## Trash and delete

```bash
proton pass items trash github.com
proton pass trash restore github.com
proton pass items delete github.com          # permanent
proton pass items trash --older-than 1y --type login --dry-run
```

`delete` and `trash empty` are permanent, so they show what would go and ask. So does a filtered `trash`, since the filter chose them rather than you. See [When it asks first](../language.md#when-it-asks-first).

## Vaults

```bash
proton pass vaults create --name Work
proton pass vaults update Work --description "Shared team logins" --icon 7 --color 3
proton pass vaults delete Work               # by name, or by share ID
```

Pass shows its icons and colours as a grid with no names, so the numbers are what there is. Deleting a vault takes everything in it, so it names the vault and asks first.

`proton pass items pin github.com` keeps an item at the top of the list.

## Aliases

Hide-my-email addresses that forward to your own mailboxes.

```bash
proton pass aliases options                   # available suffixes and mailboxes
proton pass aliases create --prefix shop --mailbox me@proton.me
```

Proton makes the address from your prefix, a word of its own, and the suffix. It picks a new word every time and only settles when the alias is made, so creating one tells you what it made:

```
✓ Created alias "shop" as shop.jasmine329@passinbox.com.
```

An alias is an item, so it is read and edited like one:

```bash
proton pass items get shop
proton pass items update shop --mailbox work@proton.me    # where its mail arrives
proton pass items update shop --display-name "Jane R"     # what recipients see
```

When an address starts attracting spam, **switch it off rather than delete it**. A disabled alias keeps its address and stops receiving; deleting it burns the address for good.

```bash
proton pass aliases disable shop
```

### Replying as an alias

An alias forwards mail to you, but a reply would leave from your real address and give it away. A contact is the answer. Proton mints a second address standing for one correspondent, and mail you send there reaches them as though the alias had written it.

```bash
proton pass aliases contacts create shopping seller@example.com --name "The seller"
proton pass aliases contacts list shopping                       # WRITE TO shows the address
proton pass aliases contacts block shopping seller@example.com
```

### Where aliases arrive

```bash
proton pass settings mailboxes create me@example.com
proton pass settings mailboxes verify me@example.com --code 123456
proton pass settings mailboxes delete me@example.com --transfer-to other@example.com
```

A new mailbox receives nothing until it answers: Proton emails it a code, and `verify` is where that code goes back. `resend` sends another and retires the one before it.

Deleting a mailbox needs somewhere for its aliases to go, which is what `--transfer-to` names. Without it, a mailbox that still has aliases is refused rather than quietly leaving them receiving nothing.

## Sharing

A vault, or one item out of it. Both read the same way, and the same way Drive's sharing does.

```bash
proton pass vaults share add Work jane@proton.me --access editor
proton pass vaults share get Work
proton pass vaults share update Work jane@proton.me --access manager
proton pass vaults share remove Work jane@proton.me

proton pass items share add github.com jane@proton.me
proton pass items share get github.com          # members, invitations and links
proton pass items share remove github.com jane@proton.me
```

A vault is opened by its share key, and every item is sealed under that key. So sharing a vault means handing over the key itself - **every rotation of it**, since an item made before the last rotation is still sealed under an older one. Sharing one item hands over that item's own key instead, so the person you share with can open it and nothing else in the vault.

The key goes out encrypted to their key and signed with yours. That is why this only works with another Proton account: an address Proton holds no keys for has nothing to encrypt to.

`--access` is `viewer`, `editor` or `manager`. `share get` shows the people who accepted as members and the rest as invited; `update` and `remove` act on the address whichever it turns out to be.

**Handing a vault over.**

```bash
proton pass vaults transfer Work jane@proton.me
```

They have to be a member already, and only the owner can do it. Afterwards you are a manager like anybody else, so it is the one change to a vault you cannot undo on your own.

### What is shared, and by whom

```bash
proton pass invitations list        # what people have offered you
proton pass invitations accept Work

proton pass shared list             # items other people share with you
proton pass sharing list            # items you share with other people
proton pass vaults list             # your vaults, with how many members each has
```

A vault's name and item count are readable before you take it - the invitation carries the key that opens that much. What is *in* it is not, until you accept. An item offered on its own carries no preview at all.

An item somebody shared with you **is in no vault of yours**, so it is not in `items list`. `shared list` has it, and it is addressed by the ID that listing shows, or by its name like anything else.

## Secure links

```bash
proton pass links create github.com --expires 7d
proton pass links create github.com --expires 24h --views 1
proton pass links list
proton pass links get 5bH2mQxK
proton pass links revoke 5bH2mQxK
```

A link that shows one item to somebody with no Proton account. The item stays encrypted, and a key made for the link is what opens it.

**That key travels in the URL after the `#`**, which a browser never sends to the server. So the URL is the secret: anyone holding the whole of it can read the item until the link expires or is revoked.

`--expires` is required. A link nobody remembered to revoke is how one of these goes wrong, and there is no sensible default for how long a secret should outlive its reason.

`create` writes the URL to stdout and the warning to stderr, so `LINK=$(proton pass links create … --expires 7d)` captures the link alone. `list` shows what links exist and leaves the URLs out, because a listing is no place for a key; `links get` reads one back whole, and `items share get` the ones an item has - so a link you mislaid is recovered rather than revoked and made again.

## Backups

```bash
proton pass export --dest pass-backup.zip --passphrase-file ~/.backup-passphrase
proton pass import pass-backup.zip --passphrase-file ~/.backup-passphrase
```

The archive is the one **Proton Pass itself writes**, so the app opens what this writes and this opens what the app wrote.

It holds **the vaults you own**. A vault somebody shared with you is theirs to back up and stays out, as it does in Proton's own export - restoring this file cannot turn their vault into a second copy under your name. When something is left out the command says how much, on stderr.

**Without a passphrase the archive holds every password in plain text**, and the command says so as it writes. With one, the document is encrypted to it and stored as `data.pgp`, which is what Proton's own importer looks for first. The passphrase comes from a file, from stdin with `--passphrase-stdin`, or from a prompt - never from a flag value ([why](../design-notes.md#why-a-password-is-never-a-flag-value)).

Importing **adds** items. Nothing in an export says which existing item it was, so importing the same file twice puts the items in twice. Items land in the vault the file names, and a vault that isn't there yet is made. `--dry-run` lists what would land, and where.

Aliases are the exception. An alias address belongs to the account Proton gave it to, so each one is named and skipped while everything else lands.

## An extra password

Pass can be protected with [an extra password](https://proton.me/support/pass-extra-password) of its own, on top of your Proton account password. Proton then refuses every Pass request from a session that has not answered it, so the first `pass` command asks:

```console
$ proton pass items list
Extra password:
ID           TYPE   NAME        USERNAME  MODIFIED
…
```

One answer covers the session, not the command: Proton grants it for as long as the session lives, so nothing asks again on this machine until you sign out or the session expires. Nothing else in proton is affected - Mail, Drive, Calendar and Contacts never ask for it.

For a run with nobody to ask, hand it to the sign-in instead:

```bash
proton account login --user me@proton.me \
  --password-file /run/secrets/proton \
  --extra-password-file /run/secrets/proton-pass
```

A `pass` command that needs it and finds nobody to ask says so and names that flag. Like every other secret it is read from a file, from stdin with `--extra-password-stdin`, or from a prompt - never from a flag value ([why](../design-notes.md#why-a-password-is-never-a-flag-value)).

Proton counts wrong answers and ends the session after a few, so a wrong one is worth reading rather than retrying blindly. Turning the extra password on or off is not something proton does - see [what proton-cli can't do](../limitations.md).

## History and breaches

```bash
proton pass items revisions list github.com    # every edit, newest first
proton pass breaches list                      # worst first
proton pass breaches get jane@proton.me
```

Pass keeps every edit, so a password changed by mistake can be read back. A revision written under a key this account no longer holds is still listed by its number.

`breaches` is Pass Monitor: which of your addresses have turned up in somebody else's data breach, when, and what was exposed. If a password leaked in the clear it shows the last few characters, which is what tells you which one to change. Nothing here writes.
