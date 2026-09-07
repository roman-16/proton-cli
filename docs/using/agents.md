# AI agents

Anything that runs shell commands can use proton, and proton can teach it how.

## Give the agent the skill

`proton skill` prints a skill in the [Agent Skills](https://agentskills.io) format: a `SKILL.md` an agent reads before it acts. Save it in a directory named `proton-cli`, wherever your agent reads its skills from:

```bash
mkdir -p /path/to/skills/proton-cli
proton skill > /path/to/skills/proton-cli/SKILL.md
```

It is written from the installed build, so it names exactly the commands your proton has, and it tells the agent to print it again when the installed version is not the one that wrote the file.

An agent can read it as it runs instead. `proton skill --body-only` leaves the frontmatter out, which is what a skill of your own wants when all it says is to run this first:

```markdown
---
name: proton-cli
description: (the description proton skill prints)
---

Before using proton, run `proton skill --body-only` and follow what it prints.
```

The skill describes the tool. What the agent may do with your account is yours to say, in its own instructions and in a confirmation policy.

Signing in stays yours: `proton account login` asks for the password and any second factor at a terminal, and no flag carries a password. The same holds for the [commands that ask for the password again](../account/README.md#commands-that-ask-for-the-password-again).

## Fence what it may do

A [confirmation policy](confirmations.md#making-more-commands-ask) is what says an agent may read your mail but never delete any:

```bash
export PROTON_CONFIRM='deletions=deny'
```

A permanent removal then exits `6` and touches nothing. `--yes` does not answer a deny, so the `--yes` the agent needs for the trashing it *is* meant to do cannot quietly authorise more.

`=deny` is the part that fences. A class written without it only makes the command ask first, and `--yes` answers that in advance.

```bash
# read-only: nothing changes, at all
export PROTON_CONFIRM='mutations=deny'

# read-only, except that it may send
export PROTON_CONFIRM='mutations=deny, mail messages send:default'
```

The full syntax is in [Writing a policy](confirmations.md#writing-a-policy).

Give it [a profile of its own](../account/README.md#more-than-one-account) if you want a way to cut it off:

```bash
proton account login --profile agent
PROTON_PROFILE=agent PROTON_CONFIRM='deletions=deny' your-agent
proton account logout --profile agent
```
