# Scripts

Everything the maintainer or CI runs, in whatever language suits it: shell installers, a TypeScript generator, a Node publisher, a Go generator, and the demo recorder.

| Directory or file | Role |
| --- | --- |
| `install.sh`, `install.ps1` | The installers users curl; referenced from the README |
| `gen-completions.sh` | Emits the shell completions shipped in releases (a goreleaser `before` hook) |
| `changelog/` | Reads `CHANGELOG.md`: the version to release and the notes to publish (`just notes`) |
| `gendocs/` | Generates the command reference from the command tree (`just docs`) |
| `openapi-generator/` | Generates `openapi.yaml` from the WebClients TypeScript source (`just openapi`) |
| `stats/` | Records the public counters the Stats page charts (`just stats`) |
| `terminal-demo/` | Records the README panel against the primary account (`just demo`) |
| `publish-npm.mjs` | Publishes the npm package on release |

## Command Reference Generator

Writes the command reference by walking the assembled Cobra tree rather than by reading the source or the prose.

One page per collection, named for the command line it documents: `docs/mail/messages.md` for `proton mail messages`, plus `docs/about/commands.md` listing every command in one table. Inside an app's directory, `README.md` is the hand-written guide and every other markdown file belongs to this generator.

```bash
just docs
```

Every command gets its whole entry: what it does, how it is invoked, what it holds, the flags it takes and the examples it already carries. All of that is on the command already, checked by conformance, and the generator is what stops it living only in a terminal - a flag nobody can search for is a flag nobody finds.

The guides are hand-written and stay that way, in files of their own. A page that is half generated cannot be regenerated, so the split is by file rather than by section.

Where a command is published is `kit.ReferencePage`'s answer, and a help screen prints the URL from the same function. A heading here and a link there cannot disagree, and `internal/cli/help_test.go` fails if a screen points at a page this does not write.

CI regenerates the whole directory and fails on a diff, so a command that exists is a command that is documented, under its current name and with its current flags.

It shares the tree with `internal/cli/conformance_test.go`, which checks the same commands against the rules the interface is meant to obey. Both call `cli.Root()`.

## Stats

Writes down what the distribution channels publish about the project, onto the `data` branch the documentation site reads at `/stats/`. Nothing here comes from the program: proton-cli has no telemetry, and this reads only counters GitHub, npm, Repology and the AUR make public.

```bash
just stats /path/to/data-branch
```

It runs at midnight UTC from `.github/workflows/stats.yml`, because two of its sources cannot be read late. A release asset's downloads are a lifetime total, so a rate exists only where one reading is subtracted from the next - which is why the first run records a baseline and no rate at all. GitHub's traffic endpoints are stricter still: they answer with the last fourteen days and delete the rest, so a fortnight without a run loses those days for good. Stars and forks are read as the repository's current totals and recorded a point a day, so their series grow forward from the first run; npm carries its own dated history and needs no catching up.

A download counts as one of two channels, because that is as far as the counters tell acquisitions apart: the unversioned executable, which both install scripts, `proton update` and a hand download all fetch, and the versioned artifact the AUR, Homebrew and winget build on. `checksums.txt` accompanies a scripted fetch rather than being one, and counts for nothing. Each row is named for the day its interval began, so a run GitHub starts late still describes the day it measured.

Those traffic endpoints require `STATS_TOKEN` - a personal access token with classic `repo` scope, or a fine-grained one with `Administration: read`. GitHub refuses them to the token a workflow is handed by default, whatever its permissions say, so the collector treats a refusal as fatal rather than recording everything except the part that expires. Any other source failing leaves its last reading in place and the run continues, which is why every source also records when it was last read and the page says which of its panels is standing still.

`state.json` is the previous reading of every cumulative counter and exists only to be subtracted from. `stats.json` is what the browser fetches.

## Changelog

The release trigger, and the only thing that reads it. `CHANGELOG.md` declares a version by carrying a section for it; this prints that version, or the release notes that go on its GitHub release page.

```bash
just notes
```

It validates the whole file before answering either question, against Keep a Changelog 1.1.0 and a little more: categories only from the six and in the specification's order, no empty sections, and versions that move one step at a time so a typed 2.30.0 is a failing test rather than a tag nobody can take back. An `[Unreleased]` section is allowed and never releasable, but this project has none: a section is written when the release is cut. `just test-fast` runs that check over the repository's own changelog, so a file that cannot be released cannot be merged.

Printing nothing is an answer: the file names no version to release, which is the state of every commit that is not a release. A `[YANKED]` section counts as nothing too, since a withdrawn release must never be republished.

## OpenAPI Generator

Auto-generates `openapi.yaml` from [ProtonMail/WebClients](https://github.com/ProtonMail/WebClients) TypeScript source files using [ts-morph](https://github.com/dsherret/ts-morph) for full AST parsing with type resolution.

### Usage

```bash
just openapi
```

This outputs `openapi.yaml` in the project root. It runs `just webclients` first, which clones `ProtonMail/WebClients` to `/tmp/proton-cli-WebClients` (~30 seconds) or updates a checkout that is already there (~1 second). Run `just webclients` on its own to refresh that checkout for reading.

### What It Extracts

Per endpoint:

| Source | OpenAPI |
|---|---|
| Function name | `operationId`, `summary` |
| `url` property | `paths` (constants resolved from source) |
| `method` property | HTTP method |
| `data` parameter type | `requestBody` schema (types resolved through imports) |
| `params` object | `parameters` (query) |
| Template literal `${vars}` | `parameters` (path) |
| `input: 'form'\|'binary'` | Request `content-type` (`multipart/form-data`, `application/octet-stream`) |
| `output: 'stream'\|'arrayBuffer'\|'text'` | Response `content-type` |
| JSDoc comments | `description` |
| `@deprecated` tag | `deprecated: true` |
| `/** Public **/` comments, `credentials: 'omit'` | `security: []` |
| `timeout` property | `x-timeout` |
| `keepalive` property | `x-keepalive` |
| `silence` array | `x-expected-errors` |
| All exported enums | Comment block in components section |

Pass declares its API as two conditional types in `packages/pass/types/api/pass.ts` rather than as one function per endpoint, so it is parsed on its own:

| Source | OpenAPI |
|---|---|
| `` Path extends `pass/v1/…` `` | `paths`, with each `${string}` named after the segment before it |
| `` Method extends `post` `` | HTTP method |
| `ApiRequestBody<Path, Method>` | `requestBody` schema |
| `ApiResponse<Path, Method>` | Response schema beside the envelope |
| Block comments above a property | `description` |

Global:

| Source | OpenAPI |
|---|---|
| All `export enum` declarations | Enum reference comments with all values |
| All `export const = 'string'` | Used to resolve URL template constants |
| TypeScript interfaces | Resolved for request body property types, optionality, and comments |

### How It Works

1. **Checkout** - `just webclients` puts the current `ProtonMail/WebClients` main in `/tmp/proton-cli-WebClients`, and the directory is passed to the generator
2. **Project setup** - creates a ts-morph `Project` with `tsconfig.base.json` for path resolution
3. **Registry** - scans all source files for string/number constants and enum declarations
4. **Parse** - walks all exported declarations in `api/**/*.ts`, extracts endpoint metadata from the AST, then walks the Pass `ApiRequestBody` and `ApiResponse` type chains
5. **Type resolution** - follows TypeScript imports to resolve `data: SomeType` to actual property lists (including `extends`, `Partial<>`, `Omit<>`, etc.)
6. **Emit** - generates OpenAPI 3.1 YAML to stdout

### File Structure

```
openapi-generator/
├── index.ts              - entry point
├── parse.ts              - ts-morph project setup, file discovery
├── registry.ts           - constant and enum collection
├── extract-endpoint.ts   - endpoint extraction from AST nodes
├── extract-params.ts     - body/query param type resolution
├── parse-pass.ts         - the Pass API, declared as conditional types
├── emit-yaml.ts          - OpenAPI YAML output
└── types.ts              - shared TypeScript interfaces
```
