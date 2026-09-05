# AI agents

Anything that runs shell commands can use proton, and proton can teach it how.

## Give the agent the skill

`proton skill` prints a skill in the [Agent Skills](https://agentskills.io) format: a `SKILL.md` an agent reads before it acts. Save it in a directory named `proton-cli`, wherever your agent reads its skills from:

```bash
mkdir -p /path/to/skills/proton-cli
proton skill > /path/to/skills/proton-cli/SKILL.md
```

It is written from the installed build, so it names exactly the commands your proton has. It also tells the agent to print it again when the version it finds is not the one that wrote the file, so a saved copy cannot quietly go stale.

It describes the tool and stops there. What the agent may do with your account is yours to say - in its own instructions, and in a [confirmation policy](#fence-what-it-may-do).

An agent can read it as it runs instead. `proton skill --body-only` leaves the frontmatter out, which is what a skill of your own wants when all it says is to run this first:

```markdown
---
name: proton-cli
description: (the description proton skill prints)
---

Before using proton, run `proton skill --body-only` and follow what it prints.
```

## What the skill says

What this build is, and nothing about how to behave with it:

- The grammar, and that `proton <command> --help` is the reference - so it reads a command's help instead of guessing a flag.
- How to check which build is installed, and which commands say who is signed in.
- That `--output json` and `--no-input` belong on everything it reads.
- How to name things: full IDs, short IDs, and handles like a subject or a Drive path.
- What an answer looks like: the envelope every listing shares, what `total` counts, what `--page-size 0` returns, and how an all-day event's `end` reads.
- That every command which changes something takes `--dry-run`, what `--yes` answers, and what each exit code means.
- How secrets go into Pass and which commands print them.
- Where every command lives, so it lands on the right `--help` in one step.

## What the skill does not say

How your agent should behave. Whether it asks you before deleting anything, what it may do while you are asleep, where it may write what it read - those are yours, they differ between setups, and instructions shipped with the tool would only argue with your own.

Say it in your agent's own instructions, and fence it with a policy below.

## What stays yours

Signing in. `proton account login` asks for the password and any second factor at a terminal, and no flag carries a password, so the agent cannot sign in for you however it is instructed.

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
