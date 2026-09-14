# proton pass breaches

Addresses that have appeared in a data breach.

Every command under `proton pass breaches`, with the arguments and flags it takes. For these commands in use, see [the pass guide](README.md).

Holds `create`, `delete`, `disable`, `enable`, `get`, `list`, `resend` and `verify`.

## `create`

Have Proton watch an address you own elsewhere.

Proton emails the address a code and watches nothing until you hand that code back with `breaches verify`.

The addresses on your account and the aliases in your vaults are watched already; this is for the ones somewhere else.

```
proton pass breaches create EMAIL
```

```bash
proton pass breaches create me@example.com
```

## `delete`

Stop Proton watching an address you added, and forget its breach history.

Only an address you added yourself can be removed. To stop Proton watching one of your own addresses or an alias, use `breaches disable`.

```
proton pass breaches delete REF
```

```bash
proton pass breaches delete me@example.com
```

## `disable`

Stop Proton watching an address.

Works on an address on your account, on an alias in one of your vaults, and on an address you added once you have verified it.

Pausing an alias also leaves it out of `items list --risk`, which is the same switch.

```
proton pass breaches disable REF
```

```bash
proton pass breaches disable jane@proton.me
```

## `enable`

Have Proton watch an address again.

Works on an address on your account, on an alias in one of your vaults, and on an address you added once you have verified it.

Pausing an alias also leaves it out of `items list --risk`, which is the same switch.

```
proton pass breaches enable REF
```

```bash
proton pass breaches enable jane@proton.me
```

## `get`

Show the breaches one address has appeared in.

Names each breach, when it happened, what it exposed, and the last few characters of the password if one leaked in the clear.

An address you added is refused until you verify it: Proton is not watching it until then.

The detail needs a plan that includes it. Without one the count is still right and the breaches are withheld, and the answer says how many.

```
proton pass breaches get REF
```

```bash
proton pass breaches get jane@proton.me
```

## `list`

List the addresses Proton watches, and how many breaches each is in.

Worst first. Three kinds of address are watched: the ones on your account, the hide-my-email aliases in your vaults, and the ones you added with `breaches create`. Listing the aliases reads your vaults, so this costs what `items list` costs.

STATE is what has to happen next: an address you added is unverified until you hand back the code Proton emailed it, and paused means watching is off.

To see which breaches an address is in and what they exposed, run `breaches get` on it.

```
proton pass breaches list
```

```bash
proton pass breaches list
```

## `resend`

Send the confirmation code again.

```
proton pass breaches resend REF
```

```bash
proton pass breaches resend me@example.com
```

## `verify`

Confirm an address with the code Proton emailed it.

```
proton pass breaches verify REF
```

```bash
proton pass breaches verify me@example.com --code 123456
```

| Flag | Description |
| --- | --- |
| `--code string` | The code Proton emailed the address |

---

Every command also takes the [flags that work everywhere](../about/commands.md#flags-that-work-on-every-command).
