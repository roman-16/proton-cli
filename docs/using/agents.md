# AI agents

Anything that runs shell commands can use proton, and proton can teach it how.

## Give the agent the skill

`proton skill` prints a skill in the [Agent Skills](https://agentskills.io) format: a `SKILL.md` an agent reads before it acts. Save it in a directory named `proton-cli`, wherever your agent reads its skills from:

```bash
mkdir -p /path/to/skills/proton-cli
proton skill > /path/to/skills/proton-cli/SKILL.md
```

It is written from the installed build, so it names exactly the commands your proton has. It also tells the agent to print it again when the version it finds is not the one that wrote the file, so a saved copy cannot quietly go stale.

An agent can read it as it runs instead. `proton skill --body-only` leaves the frontmatter out, which is what a skill of your own wants when all it says is to run this first:

```markdown
---
name: proton-cli
description: (the description proton skill prints)
---

Before using proton, run proton skill --body-only and follow what it prints.
```

## What the agent is taught

- To check that proton is installed and that somebody is signed in, before anything else.
- The grammar, and that `proton <command> --help` is the reference - so it reads a command's help instead of guessing a flag.
- To pass `--output json` and `--no-input` on everything it reads.
- How to name things: full IDs, short IDs, and handles like a subject or a Drive path.
- To preview every change with `--dry-run`, show you the result, and pass `--yes` only once you have agreed to that change.
- That exit `6` is your confirmation policy refusing the command, and is not to be worked around.
- That secrets from Pass are yours: shown when you ask, written nowhere else.
- Where every command lives, so it lands on the right `--help` in one step.

## What stays yours

Signing in. The agent is told to stop and ask you when nobody is signed in, and never to handle a password.

That leaves the [commands Proton asks for the password again on](scripting.md#commands-that-ask-for-the-password-again) as ones the agent cannot finish on its own either.

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

## Is there an MCP server?

No, and there is not likely to be one.

The CLI is already the interface an agent needs: one envelope shape for every listing, exit codes that mean something, `--dry-run` on everything that changes state, and a confirmation policy you control. An MCP server in front of it would restate every command's help as a tool schema, in a second place that can disagree with the first.

What an agent is missing is not a protocol but the instructions, and that is what `proton skill` hands it.
