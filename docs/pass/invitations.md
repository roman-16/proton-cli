# proton pass invitations

What other people have offered you.

Every command under `proton pass invitations`, with the arguments and flags it takes. For these commands in use, see [the pass guide](README.md).

Holds `accept`, `decline` and `list`.

## `accept`

Take what somebody offered you.

The keys arrive encrypted to the address the offer was sent to, and are re-encrypted to your own. A vault then behaves like any other of yours.

An item accepted on its own is in no vault of yours, so `shared list` is where it appears.

```
proton pass invitations accept REF...
```

```bash
proton pass invitations accept Work
```

## `decline`

Turn down what somebody offered you.

```
proton pass invitations decline REF...
```

```bash
proton pass invitations decline Work
```

## `list`

List what other people have offered you.

For a vault, you can read its name and how much is in it before accepting. Its contents stay sealed until you do.

An item offered on its own shows no preview at all.

```
proton pass invitations list
```

```bash
proton pass invitations list
```

---

Every command also takes the [flags that work everywhere](../about/commands.md#flags-that-work-on-every-command).
