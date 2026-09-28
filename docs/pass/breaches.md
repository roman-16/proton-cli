# proton pass breaches

Addresses that have appeared in a data breach.

Every command under `proton pass breaches`, with the arguments and flags it takes. For these commands in use, see [the pass guide](README.md).

Holds `create`, `delete`, `disable`, `enable`, `get`, `list`, `resend`, `resolve` and `verify`.

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

--type proton or --type alias switches watching for every address of that kind at once, on a paid Pass plan. While it is off, no single one can be switched, and turning it back on leaves the ones you paused one at a time paused.

```
proton pass breaches disable [REF]
```

```bash
proton pass breaches disable jane@proton.me
proton pass breaches disable --type alias
```

| Flag | Description |
| --- | --- |
| `--type string` | Every address of this kind at once, instead of one: proton, custom, alias |

## `enable`

Have Proton watch an address again.

Works on an address on your account, on an alias in one of your vaults, and on an address you added once you have verified it.

Pausing an alias also leaves it out of `items list --risk`, which is the same switch.

--type proton or --type alias switches watching for every address of that kind at once, on a paid Pass plan. While it is off, no single one can be switched, and turning it back on leaves the ones you paused one at a time paused.

```
proton pass breaches enable [REF]
```

```bash
proton pass breaches enable jane@proton.me
proton pass breaches enable --type alias
```

| Flag | Description |
| --- | --- |
| `--type string` | Every address of this kind at once, instead of one: proton, custom, alias |

## `get`

Show the breaches one address has appeared in.

Names each breach, when it happened, what it exposed, whether it is resolved, and the last few characters of the password if one leaked in the clear.

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

STATE is what has to happen next: an address you added is unverified until you hand back the code Proton emailed it, and paused means watching is off, for the address itself or for every address of its kind.

To see which breaches an address is in and what they exposed, run `breaches get` on it.

```
proton pass breaches list
```

```bash
proton pass breaches list
proton pass breaches list --type alias
```

| Flag | Description |
| --- | --- |
| `--desc` | Reverse the order |
| `--limit int` | How many watched addresses per page; 0 for all of them |
| `--page int` | Which page of results, counting from zero |
| `--sort string` | Order by: breaches, email (default `breaches`) |
| `--type string` | List only addresses of this kind: proton, custom, alias |

## `resend`

Send the confirmation code again.

```
proton pass breaches resend REF
```

```bash
proton pass breaches resend me@example.com
```

## `resolve`

Mark an address's current breaches as resolved.

Pass has no way to reopen them. A breach found later is reported as new.

Resolving needs a plan that includes the detail of each breach.

```
proton pass breaches resolve REF...
```

```bash
proton pass breaches resolve jane@proton.me
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
