# proton contacts emails

How mail to a contact's addresses is sent.

Each address has its own format, encryption, signing and scheme. Where an address says default, it follows the account's `mail settings` sign and pgp-scheme.

Every command under `proton contacts emails`, with the arguments and flags it takes. For these commands in use, see [the contacts guide](README.md).

Holds `list` and `update`.

## `list`

List a contact's addresses and how mail to each is sent.

ENCRYPT is blank for an address with no trusted key that states no choice: mail to it is encrypted when its provider publishes a key.

```
proton contacts emails list REF
```

```bash
proton contacts emails list jane
```

## `update`

Change how mail to one of a contact's addresses is sent.

Name the address as REF when the contact holds more than one. A Proton address takes --email-format alone: mail to it is always encrypted and signed. Encrypted mail is always signed, and the scheme decides the format of signed mail: plain text under pgp-inline, as written under pgp-mime. --encrypt needs a trusted key, or one the address's provider publishes.

```
proton contacts emails update REF
```

```bash
proton contacts emails update jane@example.com --email-format plain-text
proton contacts emails update jane@example.com --sign on --scheme pgp-inline
proton contacts emails update jane@example.com --encrypt off
```

| Flag | Description |
| --- | --- |
| `--email-format string` | What mail to the address is sent as: automatic, plain-text |
| `--encrypt string` | Whether mail to the address is encrypted: off, on |
| `--scheme string` | How signed or encrypted mail to the address is packaged: pgp-mime, pgp-inline, default |
| `--sign string` | Whether mail to the address is signed: on, off, default |

---

Every command also takes the [flags that work everywhere](../about/commands.md#flags-that-work-on-every-command).
