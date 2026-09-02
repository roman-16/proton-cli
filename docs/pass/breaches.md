# proton pass breaches

Addresses that have appeared in a data breach.

Every command under `proton pass breaches`, with the arguments and flags it takes. For these commands in use, see [the pass guide](README.md).

Holds `get` and `list`.

## `get`

Show the breaches one address has appeared in.

```
proton pass breaches get REF
```

```bash
proton pass breaches get jane@proton.me
```

## `list`

List the addresses Proton watches, and how many breaches each is in.

Worst first. To see which breaches an address is in and what they exposed, run `breaches get` on it.

```
proton pass breaches list
```

```bash
proton pass breaches list
```

---

Every command also takes the [flags that work everywhere](../about/commands.md#flags-that-work-on-every-command).
