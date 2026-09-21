# proton account settings

Account-wide preferences.

Every command under `proton account settings`, with the arguments and flags it takes. For these commands in use, see [the account guide](README.md).

Holds `get`, `list`, `password`, `recovery-email`, `recovery-phone`, `recovery-phrase`, `second-password`, `security-keys`, `set` and `two-factor`.

## `get`

Show the account settings now in effect.

```
proton account settings get
```

```bash
proton account settings get
```

## `list`

List the account settings that can be changed.

```
proton account settings list
```

```bash
proton account settings list
```

## `password`

The password you sign in with.

Holds `set`.

### `password set`

Change your password.

Your current password is asked for first. The new one comes from --new-password-file, which takes - for stdin, or from a prompt that asks for it twice. It needs at least eight characters and cannot be the one it replaces.

In two-password mode this changes the password you sign in with, and your keys stay locked with your second password.

This session goes on working. Another machine signed in to this account asks for the new password the next time it opens your keys.

```
proton account settings password set
```

```bash
proton account settings password set
proton account settings password set --password-file /run/secrets/proton --new-password-file /run/secrets/proton-new
```

| Flag | Description |
| --- | --- |
| `--new-password-file string` | Read the password being set from a file, or - for stdin |
| `--password-file string` | Read the account password from a file, or - for stdin |
| `--totp string` | Two-factor code |

## `recovery-email`

The address Proton writes to if you lose access.

Holds `disable`, `enable`, `get`, `set` and `verify`.

### `recovery-email disable`

Stop allowing password resets by email.

Your password is asked for. The address stays, and Proton goes on sending security notices to it.

```
proton account settings recovery-email disable
```

```bash
proton account settings recovery-email disable
```

| Flag | Description |
| --- | --- |
| `--password-file string` | Read the account password from a file, or - for stdin |
| `--totp string` | Two-factor code |

### `recovery-email enable`

Allow password resets by email.

Your password is asked for. An account with no recovery address, and one with Proton Sentinel turned on, is refused.

```
proton account settings recovery-email enable
```

```bash
proton account settings recovery-email enable
```

| Flag | Description |
| --- | --- |
| `--password-file string` | Read the account password from a file, or - for stdin |
| `--totp string` | Two-factor code |

### `recovery-email get`

Show the recovery address and what it may do.

```
proton account settings recovery-email get
```

```bash
proton account settings recovery-email get
```

### `recovery-email set`

Set the recovery address.

Your password is asked for. `set none` removes the address, and with it any way to reset your password by email.

A new address is unverified until you open the link Proton mails to it: `proton account settings recovery-email verify` sends one.

```
proton account settings recovery-email set EMAIL
```

```bash
proton account settings recovery-email set jane.roe@example.com
proton account settings recovery-email set none
```

| Flag | Description |
| --- | --- |
| `--password-file string` | Read the account password from a file, or - for stdin |
| `--totp string` | Two-factor code |

### `recovery-email verify`

Send a verification email to the recovery address.

Open the link in it to finish. Until then Proton will not reset your password by email.

```
proton account settings recovery-email verify
```

```bash
proton account settings recovery-email verify
```

## `recovery-phone`

The number Proton texts if you lose access.

Holds `disable`, `enable`, `get`, `set` and `verify`.

### `recovery-phone disable`

Stop allowing password resets by SMS.

Your password is asked for. The number stays, and Proton goes on using it for security notices.

```
proton account settings recovery-phone disable
```

```bash
proton account settings recovery-phone disable
```

| Flag | Description |
| --- | --- |
| `--password-file string` | Read the account password from a file, or - for stdin |
| `--totp string` | Two-factor code |

### `recovery-phone enable`

Allow password resets by SMS.

Your password is asked for. An account with no recovery number, and one with Proton Sentinel turned on, is refused.

```
proton account settings recovery-phone enable
```

```bash
proton account settings recovery-phone enable
```

| Flag | Description |
| --- | --- |
| `--password-file string` | Read the account password from a file, or - for stdin |
| `--totp string` | Two-factor code |

### `recovery-phone get`

Show the recovery number and what it may do.

```
proton account settings recovery-phone get
```

```bash
proton account settings recovery-phone get
```

### `recovery-phone set`

Set the recovery number.

Your password is asked for. The number needs its country code, as +43 660 1234567. `set none` removes the number, and with it any way to reset your password by SMS.

A new number is unverified until a code Proton texts it is handed back: `proton account settings recovery-phone verify` sends one.

```
proton account settings recovery-phone set PHONE
```

```bash
proton account settings recovery-phone set '+43 660 1234567'
proton account settings recovery-phone set none
```

| Flag | Description |
| --- | --- |
| `--password-file string` | Read the account password from a file, or - for stdin |
| `--totp string` | Two-factor code |

### `recovery-phone verify`

Confirm the recovery number with a code sent by SMS.

With no --code, Proton texts the number a code. Run it again with --code to hand the code back, which is what marks the number verified.

Until then Proton will not reset your password by SMS.

```
proton account settings recovery-phone verify
```

```bash
proton account settings recovery-phone verify
proton account settings recovery-phone verify --code 482913
```

| Flag | Description |
| --- | --- |
| `--code string` | The code Proton texted the number |

## `recovery-phrase`

The twelve words that open your account without your password.

Holds `disable`, `get` and `set`.

### `recovery-phrase disable`

Remove the recovery phrase.

Your password is asked for. The words you wrote down stop working, and nothing else recovers the account through them.

```
proton account settings recovery-phrase disable
```

```bash
proton account settings recovery-phrase disable
```

| Flag | Description |
| --- | --- |
| `--password-file string` | Read the account password from a file, or - for stdin |
| `--totp string` | Two-factor code |

### `recovery-phrase get`

Show whether a recovery phrase is set.

The status is one of: on, outdated, not set, off. An outdated phrase was set before your keys were replaced: it still opens the data it was made for, and it will not get you back into the account.

```
proton account settings recovery-phrase get
```

```bash
proton account settings recovery-phrase get
```

### `recovery-phrase set`

Make a recovery phrase and print it.

Your password is asked for. The twelve words are printed once and kept nowhere: write them down. Anyone holding them can open the account.

A phrase that is already set is replaced and stops working.

Recover with `proton account keys reactivate --recovery-phrase`.

```
proton account settings recovery-phrase set
```

```bash
proton account settings recovery-phrase set
proton account settings recovery-phrase set --output json
```

| Flag | Description |
| --- | --- |
| `--password-file string` | Read the account password from a file, or - for stdin |
| `--totp string` | Two-factor code |

## `second-password`

The second password your keys can be locked with.

In two-password mode one password signs you in and another opens your keys.

Holds `disable`, `enable`, `get` and `set`.

### `second-password disable`

Leave two-password mode.

Your password is asked for, and your keys are locked with it. The second password stops working, and nothing else about the account changes.

This session goes on working. Another machine signed in to this account asks for your password the next time it opens your keys.

```
proton account settings second-password disable
```

```bash
proton account settings second-password disable
```

| Flag | Description |
| --- | --- |
| `--password-file string` | Read the account password from a file, or - for stdin |
| `--totp string` | Two-factor code |

### `second-password enable`

Lock your keys with a second password.

The password you sign in with is asked for first and stays as it is. The second password comes from --new-password-file, which takes - for stdin, or from a prompt that asks for it twice. It needs at least eight characters and cannot be the one you sign in with.

Signing in then takes both: `proton account login --second-password-file`, or a prompt for the second one after your password.

An account already in two-password mode is refused.

```
proton account settings second-password enable
```

```bash
proton account settings second-password enable
proton account settings second-password enable --password-file /run/secrets/proton --new-password-file /run/secrets/proton-second
```

| Flag | Description |
| --- | --- |
| `--new-password-file string` | Read the password being set from a file, or - for stdin |
| `--password-file string` | Read the account password from a file, or - for stdin |
| `--totp string` | Two-factor code |

### `second-password get`

Show whether this account uses two-password mode.

```
proton account settings second-password get
```

```bash
proton account settings second-password get
```

### `second-password set`

Change your second password.

The password you sign in with is asked for first. The new second password comes from --new-password-file, which takes - for stdin, or from a prompt that asks for it twice. It needs at least eight characters and cannot be the one you sign in with.

Your keys are locked with it from then on. This session goes on working; another machine signed in to this account asks for it the next time it opens your keys.

An account that does not use two-password mode is refused.

```
proton account settings second-password set
```

```bash
proton account settings second-password set
proton account settings second-password set --password-file /run/secrets/proton --new-password-file /run/secrets/proton-second
```

| Flag | Description |
| --- | --- |
| `--new-password-file string` | Read the password being set from a file, or - for stdin |
| `--password-file string` | Read the account password from a file, or - for stdin |
| `--totp string` | Two-factor code |

## `security-keys`

The security keys this account signs in with.

A key is one you plug in. Registering it asks you to touch it, so it takes somebody at the machine; a built-in authenticator and a passkey held in a phone are registered at https://account.proton.me instead.

An account may have four. Registering the first one makes a key part of every sign-in, and removing the last one ends that.

Holds `create`, `delete`, `list` and `update`.

### `security-keys create`

Register a security key with the account.

Your password is asked for, then the key: plug it in and touch it, and give its PIN if it has one. A key already registered here is refused by the key itself.

--name is required and is what the key is listed under. An account may have four keys, and from the first one a key is asked for at every sign-in.

With no terminal to ask, pass --password-file, which takes - for stdin. The touch still needs you there.

```
proton account settings security-keys create
```

```bash
proton account settings security-keys create --name "YubiKey 5C"
proton account settings security-keys create --name "Spare key" --password-file /run/secrets/proton
```

| Flag | Description |
| --- | --- |
| `--name string` | Name for the new security key |
| `--password-file string` | Read the account password from a file, or - for stdin |
| `--totp string` | Two-factor code |

### `security-keys delete`

Remove a security key from the account.

The key stops signing in here and keeps the credential it made, which only its own manufacturer's tool clears. Removing the last one stops a key being asked for at all.

Asks for your password even when you are signed in. With no terminal to ask, pass --password-file, which takes - for stdin.

```
proton account settings security-keys delete REF
```

```bash
proton account settings security-keys delete "Key in the safe"
proton account settings security-keys delete 5bH2mQxK --yes --password-file /run/secrets/proton
```

| Flag | Description |
| --- | --- |
| `--password-file string` | Read the account password from a file, or - for stdin |
| `--totp string` | Two-factor code |

### `security-keys list`

List the security keys registered with the account.

The ID is what `update` and `delete` take, and a key's name works in place of it.

```
proton account settings security-keys list
```

```bash
proton account settings security-keys list
```

| Flag | Description |
| --- | --- |
| `--desc` | Reverse the order |
| `--limit int` | How many security keys per page; 0 for all of them |
| `--page int` | Which page of results, counting from zero |
| `--sort string` | Order by: name (default `name`) |

### `security-keys update`

Rename a security key.

The name is how you tell your keys apart and is all that changes: the key signs in exactly as it did.

```
proton account settings security-keys update REF
```

```bash
proton account settings security-keys update "Spare key" --name "Key in the safe"
```

| Flag | Description |
| --- | --- |
| `--name string` | New name for the security key |

## `set`

Change one account setting.

```
proton account settings set KEY VALUE
```

```bash
proton account settings set locale de_AT
proton account settings set week-start monday
```

## `two-factor`

What you are asked for at sign-in besides your password.

Holds `disable`, `enable`, `generate` and `get`.

### `two-factor disable`

Stop asking for an authenticator app code.

Your password and a current code are asked for. A recovery code works in place of the code.

A registered security key goes on being asked for.

```
proton account settings two-factor disable
```

```bash
proton account settings two-factor disable
proton account settings two-factor disable --totp 123456 --password-file /run/secrets/proton
```

| Flag | Description |
| --- | --- |
| `--password-file string` | Read the account password from a file, or - for stdin |
| `--totp string` | Two-factor code |

### `two-factor enable`

Ask for an authenticator app code at every sign-in.

A secret is printed and you are asked for a code from it. With --totp CODE the code confirms the secret from the last `two-factor generate` instead, which is how this runs with nobody to ask.

Recovery codes are printed once as it turns on. Each signs you in a single time if you lose the app.

Security keys are registered with `proton account settings security-keys create`.

```
proton account settings two-factor enable
```

```bash
proton account settings two-factor enable
proton account settings two-factor enable --totp 123456 --password-file /run/secrets/proton
```

| Flag | Description |
| --- | --- |
| `--password-file string` | Read the account password from a file, or - for stdin |
| `--totp string` | Two-factor code |

### `two-factor generate`

Make a two-factor secret for an authenticator app.

Nothing about the account changes. The secret waits until `proton account settings two-factor enable --totp CODE` confirms it with a code computed from it, and making another replaces the one waiting.

It is printed once. Anyone holding it can produce your codes.

```
proton account settings two-factor generate
```

```bash
proton account settings two-factor generate
proton account settings two-factor generate --output json
```

### `two-factor get`

Show what this account is asked for at sign-in.

```
proton account settings two-factor get
```

```bash
proton account settings two-factor get
```

---

Every command also takes the [flags that work everywhere](../about/commands.md#flags-that-work-on-every-command).
