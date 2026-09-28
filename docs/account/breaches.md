# proton account breaches

Breaches Dark Web Monitoring found your addresses in.

A breach is new until you read it in a Proton app, open until you resolve it, and resolved until you reopen it. The detail of each breach needs a plan that includes Dark Web Monitoring.

The addresses Pass Monitor watches, aliases and addresses you added included, are under `proton pass breaches`.

Every command under `proton account breaches`, with the arguments and flags it takes. For these commands in use, see [the account guide](README.md).

Holds `disable`, `enable`, `get`, `list`, `reopen` and `resolve`.

## `disable`

Turn Dark Web Monitoring off.

--emails stops only the emails it sends, and leaves it on.

```
proton account breaches disable
```

```bash
proton account breaches disable
proton account breaches disable --emails
```

| Flag | Description |
| --- | --- |
| `--emails` | Only the emails it sends |

## `enable`

Turn Dark Web Monitoring on.

--emails turns on the emails it sends about what it finds as well, and turns it on first if it was off.

```
proton account breaches enable
```

```bash
proton account breaches enable
proton account breaches enable --emails
```

| Flag | Description |
| --- | --- |
| `--emails` | Also the emails it sends |

## `get`

Show one breach, and what it exposed.

Names what leaked, the last few characters of the password if one leaked in the clear, and what Proton recommends doing about it.

```
proton account breaches get REF
```

```bash
proton account breaches get Canva
```

## `list`

List the breaches Proton found your addresses in, open ones first.

Without a plan that includes Dark Web Monitoring, Proton names a few of them and withholds the rest, and the answer says how many.

```
proton account breaches list
```

```bash
proton account breaches list
proton account breaches list --output json
```

| Flag | Description |
| --- | --- |
| `--limit int` | How many breaches per page; 0 for all of them |
| `--page int` | Which page of results, counting from zero |

## `reopen`

Mark resolved breaches as open again.

```
proton account breaches reopen REF...
```

```bash
proton account breaches reopen Canva
```

## `resolve`

Mark breaches as resolved.

```
proton account breaches resolve REF...
```

```bash
proton account breaches resolve Canva
proton account breaches resolve 7Hq2Lm9x Pz81Kd0a
```

---

Every command also takes the [flags that work everywhere](../about/commands.md#flags-that-work-on-every-command).
