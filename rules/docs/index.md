# How legible scores a spec

An OpenAPI document that validates is not necessarily one an agent can use.
When an LLM calls an API, each operation becomes a tool: the operationId is
its name, the description is how it gets chosen, the parameters and request
body become one argument object, and the responses are what it reasons about
afterwards. Each of those steps has a way to go wrong that a validator will
never flag, because the document is technically fine.

legible checks for those. It runs offline, needs no account, and every rule
has a page like this one: `legible explain <rule>`.

## Categories

- **descriptions**: can an agent tell what each operation does, and pick the
  right one? This is by far the biggest lever. A model chooses tools almost
  entirely from their descriptions.
- **naming**: do operations have stable, valid tool names, and does the API
  spell things one way?
- **errors**: when a call fails, does the agent learn why and what to do?
- **examples**: can the model see what a valid call and a real response look
  like?
- **tool-schema**: can the operation be turned into a tool definition at all,
  and one that a model will fill in correctly?
- **safety**: does the operation say what credentials it needs?

## The score

Each rule records every place it looked, not just every place it found a
problem. A rule's score is the share of those places that passed. A spec
with 3 undocumented parameters out of 200 is in much better shape than one
with 3 out of 4, and a bare count of findings cannot tell those apart.

A few rules count some places for less. `property-description` and
`untyped-property` weight a property by its depth: 1 at the top level, ½ one
level down, ¼ below that. A field three objects down matters less to an agent
than an argument it fills on every call. Everything is still reported; only
the score is weighted.

A category's score is the average of its rules' scores, weighted by severity:
error rules count three times as much as info rules. The overall score is the
plain average of the categories.

A rule with nothing to look at does not count. A spec with no request bodies
neither gains nor loses on request examples. A category where no rule applied
shows as `n/a` and is left out of the overall score.

| Score | Grade |
|---|---|
| 90 and up | agent-ready |
| 75 to 89 | usable, with friction |
| 50 to 74 | agents will struggle |
| below 50 | not agent-ready |

## Severities

- **error**: stops an agent outright, or makes the operation impossible to
  expose as a tool without hand editing. For example, an operationId that no
  tool-calling API accepts as a name.
- **warning**: makes it more likely an agent picks the wrong operation or
  sends the wrong arguments.
- **info**: worth fixing, but rarely stops anything on its own.

`legible check` exits 1 when a finding is at least as severe as `--fail-on`
(default `error`), when the score is below `--fail-under`, or, with
`--fail-on-skipped`, when a remote file could not be fetched and part of the
spec went unchecked. It exits 2 when it
could not check at all, so CI can tell "fix the spec" from "fix the
pipeline".

## $refs

legible follows `$ref`s as OpenAPI defines them, so a spec split across
files is checked as a whole:

- `#/components/schemas/Pet` resolves in the file the ref is written in.
- `schemas/pet.yaml#/Pet` resolves relative to the directory of the file
  the ref is written in, not the root document's.
- `https://…` is fetched, with a 30 second timeout and a 32 MB limit.
  `--offline`, or `allow-remote: false` in config, stops that; stderr
  then says how many files were skipped.

Fetched files are cached in `.legible/cache/`, beside `.legible.yaml`, or at
the repository root when there is no config file. The directory is created on
the first fetch, with a `.gitignore` that keeps it out of version control.
With a cached copy:

- **A fresh copy is used without asking.** Freshness comes from the
  response's headers: `max-age` or `Expires` sets it, `no-cache` means ask
  every time, and `no-store` means don't keep it at all.
- **Otherwise the copy is revalidated** with a conditional request. If the
  server answers "not modified", nothing is downloaded, and the copy is
  fresh again for as long as that answer says.
- **A server that sends no validators and no caching headers** gives legible
  no cheap way to ask whether a file has changed. Those copies are used for
  an hour before being downloaded again.
- **An unreachable server or a 5xx** is covered by the cached copy, and
  stderr says it was used.
- **A 404 is still an error.** A file that has been deleted should not be
  hidden behind an old copy.
- **`--offline` reads the cache** rather than skipping the ref, so an
  air-gapped CI job can check what an earlier run fetched.

`--refresh` revalidates every cached file now, however fresh it is.
`--no-cache` neither reads nor writes the cache.

## Long reports

Operations are listed worst first: most errors, then most warnings, then most
info. An error means the operation can't be used as a tool at all, so one
error outranks any number of warnings. Findings that belong to no operation,
such as a broken ref, come before all of them. The end of the text report
repeats the five worst operations, and `--summary` shows only the scores,
the ten worst operations and a count per rule.

In text and Markdown, each operation lists at most three findings per rule
and counts the rest: `… 12 more property-description`. A big spec can have
dozens of near-identical nested findings in one operation, and they would
bury the one that matters. `--verbose` lists everything. JSON and SARIF
always carry every finding.

Markdown is also bounded in size, because GitHub caps a comment at 65,536
characters. It opens with a table counting every finding by rule, then lists
at most 100 findings, most severe first.

Findings in other files name the file: `schemas/pet.yaml:3:5`. A ref that
does not resolve is a finding of its own; see `unresolved-ref`.

A remote file legible could not get is **skipped**, not failed. Only a 404 or
410 counts against the spec; a network error, timeout, 5xx, 401/403 or
`--offline` with no cached copy does not. Every report then lists what was
not checked, the grade is marked incomplete, and JSON says `"complete":
false`. When the output is JSON or SARIF, stderr says so as well.

By default a skip does not fail the gate: a flaky network should not break
a build over a spec that has not changed. Where an incomplete check should
count as a failure, pass `--fail-on-skipped` or set `fail-on-skipped: true`.
It exits 1 and says why on stderr. Combined with `allow-remote: false`, it
means every remote file must already be in the cache, which suits a CI job
with no network.

## Configuration

legible reads `.legible.yaml` (or `.legible.yml`) from the working
directory, or the nearest parent up to the repository root. `--config`
names a file instead, and `--no-config` ignores it. Flags override the file.

```yaml
fail-on: warning        # error (default), warning, info or none
fail-under: 75          # minimum score
allow-remote: true      # follow $refs to URLs (default true)
fail-on-skipped: false  # fail if a remote file could not be fetched

rules:
  property-description: off     # don't run it
  error-shape: error            # change its severity
  parameter-count:              # change its severity and options
    severity: info
    max: 30

ignore:
  - rule: parameter-count
    operations:
      - GET /v1/transactions
      - GET /v1/stats/*         # * matches within one path segment
    reason: >
      Wide search endpoints; every filter is a real one.
  - rule: property-description
    files: ["schemas/vendor/**"] # ** matches any number of directories
    reason: Vendored from upstream.
```

An ignore names `operations`, `files`, or both; with both, a finding must
match both. File patterns are relative to the config file's directory, so
they mean the same file wherever legible is run from. The root document is a
file too, and URLs are matched as written. Use `files` for findings in shared
schemas, which belong to no single operation.

- **Unknown keys, rules and options are errors.** A misspelt setting that
  silently does nothing is a gate someone believes is on.
- **Every ignore needs a reason.** An ignore nobody can explain is one
  nobody can safely delete.
- **An ignored finding counts as a pass.** An ignore records a decision
  that the finding is acceptable, so the score reflects the spec as
  accepted. Reports still show how many findings were ignored, and SARIF
  marks them as suppressed rather than leaving them out.
- **An ignore that matches nothing is reported** on stderr, because it would
  silently swallow the problem if it ever came back.

Options are listed on each rule's page and by `legible rules`.

## What legible does not do

- **Validate.** Run a validator as well. legible assumes the document parses
  and does not repeat the checks a validator makes.
- **Call the API.** Everything is read from the document. A spec that says
  one thing while the server does another will score well here and fail in
  production.
- **Invent constraints.** No rule asks for a `pattern` or `maxLength` on a
  field whose data does not have one. A constraint added to please a linter
  rejects valid input.

When a rule is wrong for your API, turn it off with `--disable <rule>`. Each
rule's page says when it is reasonable to do that.
