# proton itself

Updating, uninstalling, shell completions, the skill an AI agent reads, and what a release changed.

These act on this installation rather than on your account, so none of them needs you to be signed in.

## `changelog`

Print the release notes, newest first: the whole changelog, one version, or the releases between two of them.

--since names the version you are on and is not included; --until names where to stop and is. Releases older than the changelog itself are on the releases page.

```
proton changelog [VERSION]
```

```bash
proton changelog
proton changelog 2.4.1
proton changelog --since 2.3.0
proton changelog --since 2.3.0 --until 2.4.0
```

| Flag | Description |
| --- | --- |
| `--since string` | Only releases after this version |
| `--until string` | Only releases up to and including this version |

## `completion`

Generate a shell completion script.

Completion knows the whole command tree, every flag, and the values each enumerated flag accepts - so it offers folder names, item types, output formats and setting keys as you type them.

Where a command takes a reference it offers back what your listings showed: the short ID, and the subject, name or address beside it. It reads what this machine remembers rather than asking Proton, so a collection you have not listed yet offers nothing and says which listing would fill it.

One script covers both proton and proton-cli.

  bash        proton completion bash > /etc/bash_completion.d/proton
  zsh         proton completion zsh > "${fpath[1]}/_proton"
  fish        proton completion fish > ~/.config/fish/completions/proton.fish
  powershell  proton completion powershell | Out-String | Invoke-Expression

```
proton completion
```

## `report`

Collect what a bug report needs: this build, your settings, and a redacted trace of the run that failed.

The last run that failed, or --all for every run still on disk. One file per day; the last 16 are kept.

Addresses, IDs and file paths are replaced by stable stand-ins before anything is written, so the same address reads as the same name throughout and as nothing at all to anybody else. Nothing here can be turned back into an address, a password, a subject or a filename.

A long run is shortened to what an issue form takes: its first and last records, and everything above debug in between. --dest writes the whole of it to a file instead.

Reads only what is already on this machine: no account, no network.

```
proton report
```

```bash
proton report
proton report --all
proton report --dest bug.txt
```

| Flag | Description |
| --- | --- |
| `--all` | Act on everything in scope, rather than a subset |
| `--dest string` | Write to this path, or - for stdout |
| `--force` | Overwrite a file that already exists |

## `skill`

Print the skill that teaches an AI agent to use proton.

A skill is a SKILL.md an agent reads before it acts (https://agentskills.io): what proton is for, how a command is shaped, what an answer looks like, the flags every call takes, and where every command lives. It describes the tool and nothing else - how the agent should behave with an account is yours to say, in your own instructions. It is written from this build, so it names exactly the commands this proton has, and it tells the agent to print it again when the installed proton is a different one.

Save it as SKILL.md inside a directory named proton-cli, wherever your agent reads skills. An agent that reads it as it runs rather than from a saved file wants --body-only, which leaves the frontmatter out.

```
proton skill
```

```bash
proton skill
proton skill --body-only
proton skill > skills/proton-cli/SKILL.md
```

| Flag | Description |
| --- | --- |
| `--body-only` | Emit only the body, with no frontmatter |

## `uninstall`

Remove a proton binary installed with the curl or PowerShell installer, or downloaded by hand.

A package-managed install (apt, dnf, apk, AUR, Homebrew, winget, npm, Nix) is refused, with the right command to use instead.

Only the binary goes, under both names it answers to. --purge also deletes your saved sessions, the ID cache and the diagnostic log.

```
proton uninstall
```

```bash
proton uninstall --dry-run
proton uninstall --yes
proton uninstall --yes --purge
```

| Flag | Description |
| --- | --- |
| `--purge` | Also remove local data (saved sessions, ID cache and diagnostic log) |

## `update`

Replace this proton binary in place with the latest GitHub release (or a specific version), verifying the download against the published SHA-256 checksums.

Only a curl-script install or a manually downloaded binary can update itself. If proton was installed with a package manager (apt, dnf, apk, Homebrew, winget, npm, Nix), update it with that package manager.

```
proton update [VERSION]
```

```bash
proton update
proton update --check
proton update 1.9.11
proton update --reinstall
```

| Flag | Description |
| --- | --- |
| `--check` | Only report whether an update is available; don't install |
| `--reinstall` | Install again even if already up to date |

## `version`

Print the version and build information.

```
proton version
```

```bash
proton version
proton version --output json
```

---

Every command also takes the [flags that work everywhere](about/commands.md#flags-that-work-on-every-command).
