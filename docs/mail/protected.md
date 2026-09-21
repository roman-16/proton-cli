# proton mail protected

Messages somebody sent you behind a password.

Every command under `proton mail protected`, with the arguments and flags it takes. For these commands in use, see [the mail guide](README.md).

Holds `attachments`, `get` and `reply`.

## `attachments`

Files attached to a password-protected message.

Holds `download` and `list`.

### `attachments download`

Download and decrypt attachments.

Naming an attachment downloads that one; naming none downloads them all. Existing files are never overwritten silently: a collision becomes "file (2).pdf" unless --force says otherwise.

```
proton mail protected attachments download LINK [ATTACHMENT_REF]
```

```bash
proton mail protected attachments download 9fK2pQ7xNv4mB8 --eo-password-file /run/secrets/jane --dest-dir .
proton mail protected attachments download 9fK2pQ7xNv4mB8 q3-report.pdf --eo-password-file /run/secrets/jane --dest report.pdf
```

| Flag | Description |
| --- | --- |
| `--dest string` | Write to this path, or - for stdout |
| `--dest-dir string` | Write into this directory, keeping each item's own name |
| `--eo-password-file string` | Read the password for recipients outside Proton from a file, or - for stdin |
| `--force` | Overwrite a file that already exists |
| `--include-inline` | Include inline attachments when downloading them all |

### `attachments list`

List a password-protected message's attachments.

```
proton mail protected attachments list LINK
```

```bash
proton mail protected attachments list 9fK2pQ7xNv4mB8 --eo-password-file /run/secrets/jane
```

| Flag | Description |
| --- | --- |
| `--eo-password-file string` | Read the password for recipients outside Proton from a file, or - for stdin |
| `--include-inline` | Include inline attachments, such as signature graphics |

## `get`

Show a password-protected message, decrypted.

LINK is the address you were sent or the id in it, and the password is the one whoever sent it gave you rather than a Proton password. Neither this nor anything else under `protected` needs an account.

Such a message is gone 28 days after it was sent, and the Expires line says when. A Replies line counts the answers already sent from behind the link.

```
proton mail protected get LINK
```

```bash
proton mail protected get 9fK2pQ7xNv4mB8 --eo-password-file /run/secrets/jane
proton mail protected get 'https://mail.proton.me/eo/9fK2pQ7xNv4mB8' --eo-password-file -
proton mail protected get 9fK2pQ7xNv4mB8 --eo-password-file /run/secrets/jane --body-only
```

| Flag | Description |
| --- | --- |
| `--body-only` | Emit only the body, with no headers or attachment list |
| `--eo-password-file string` | Read the password for recipients outside Proton from a file, or - for stdin |
| `--include-inline` | List inline attachments too, such as signature graphics |
| `--render string` | Which representation of the body to print: text, html, raw (default `text`) |
| `--strip-quotes` | Drop quoted reply blocks from the body |

## `reply`

Reply to a password-protected message.

The answer goes to whoever sent it and nobody else - the link knows no other address - sealed to their key, with the original quoted below your text.

Proton takes five answers from behind one link. --eo-password-file - claims standard input, so it cannot be combined with --body -.

```
proton mail protected reply LINK
```

```bash
proton mail protected reply 9fK2pQ7xNv4mB8 --eo-password-file /run/secrets/jane --body 'Got them, thanks.'
proton mail protected reply 9fK2pQ7xNv4mB8 --eo-password-file /run/secrets/jane --body 'Signed copy attached.' --attach ./signed.pdf
```

| Flag | Description |
| --- | --- |
| `--attach stringArray` | File to attach (repeatable) |
| `--body string` | Your text, placed above the quoted original (- reads stdin) |
| `--eo-password-file string` | Read the password for recipients outside Proton from a file, or - for stdin |
| `--html` | Compose in HTML (default: match the original) |
| `--no-quote` | Do not quote the original message |

---

Every command also takes the [flags that work everywhere](../about/commands.md#flags-that-work-on-every-command).
