# legible

Is your API legible to an agent?

legible reads an OpenAPI 3.x document and reports what will trip up an LLM or
agent calling it as a set of tools: vague descriptions, inconsistent naming,
unhelpful errors, missing examples, and schemas that are hard to turn into
tool definitions. It scores the spec, points at the line, and explains every
finding.

This is the real output for the
[Swagger Petstore](https://petstore3.swagger.io/api/v3/openapi.json), trimmed.
The spec is minified JSON, which is why every finding is on line 1; the
column tells them apart.

```console
$ legible check petstore.json

PUT /user/{username}
     1:13250 error   arguments collide when flattened into one tool input: "username" (path and body) argument-collision
     1:13250 warning no security requirement, here or at the top level: an agent cannot tell whether this needs credentials, or which security-declared
     1:13315 warning described identically to 2 other operations (POST /user, PUT /user/{username}, DELETE /user/{username}): an agent cannot tell them apart duplicate-description
  ...

legible  petstore.json, OpenAPI 3.0.4, 19 operations

  descriptions      62  ████████████░░░░░░░░
  naming           100  ████████████████████
  errors            50  ██████████░░░░░░░░░░
  examples          32  ██████░░░░░░░░░░░░░░
  tool-schema       99  ████████████████████
  safety            47  █████████░░░░░░░░░░░

  score             65  agents will struggle
```

One binary, with no service and no account. Your spec never leaves the
machine: legible sends nothing anywhere. It does fetch `$ref`s that point at
URLs, and `--offline` turns that off.

## Install

```bash
go install github.com/JustSteveKing/legible/cmd/legible@latest
```

Or download a binary from the [releases](https://github.com/JustSteveKing/legible/releases).
Linux, macOS and Windows.

## Use

```bash
legible check openapi.yaml                  # findings and a score
legible check openapi.json -f json          # for scripts
legible check openapi.yaml -f sarif > legible.sarif   # for GitHub code scanning
legible check openapi.yaml -f markdown      # for a PR comment or job summary
legible check openapi.yaml --verbose        # every finding, not three per rule per operation
legible check openapi.yaml --summary        # scores, worst operations, counts per rule
legible check https://example.com/openapi.json
curl -s https://example.com/openapi.json | legible check -

legible rules                               # every rule, its category, severity and options
legible explain                             # how the score works
legible explain argument-collision          # why a rule matters and how to fix it
```

### In CI

`legible check` exits:

| Code | Meaning |
|---|---|
| 0 | checked, and passed the gate |
| 1 | checked, and failed the gate: fix the spec |
| 2 | could not check (missing file, not OpenAPI 3, bad flag): fix the pipeline |

The gate is `--fail-on error` by default: any error-severity finding fails.
Tighten it with `--fail-on warning`, add `--fail-under 80` to hold a minimum
score, or use `--fail-on none` to report without failing. `--fail-on-skipped`
also fails a run in which a remote `$ref` couldn't be fetched, so part of the
spec went unchecked.

```yaml
- run: go install github.com/JustSteveKing/legible/cmd/legible@latest
- run: legible check openapi.yaml -f sarif --fail-on none > legible.sarif
- uses: github/codeql-action/upload-sarif@v4
  with: { sarif_file: legible.sarif }
- run: legible check openapi.yaml --fail-under 75
```

### Configuration

Commit a `.legible.yaml` next to your spec, or anywhere up to the repository
root:

```yaml
fail-on: warning
fail-under: 75

rules:
  property-description: off
  parameter-count:
    severity: info
    max: 30

ignore:
  - rule: parameter-count
    operations: ["GET /v1/transactions", "GET /v1/stats/*"]
    reason: Wide search endpoints; every filter is a real one.
  - rule: unresolved-ref
    files: ["schemas/vendor/**"]
    reason: Vendored from upstream; reported to them.
```

Rules can be turned off, given a different severity, or given different
thresholds. Specific findings can be accepted with a reason. An accepted
finding counts as a pass, and SARIF reports it as suppressed rather than
leaving it out. Unknown keys, rules and options are errors, so a typo can't
switch a check off without anyone noticing. `legible explain` covers the
whole format, and each rule's guide page ends with **When to ignore it**.

For one-off runs, `--disable` and `--only` take rule names, and
`--fail-on`, `--fail-under` and `--offline` override the file.

### Specs split across files

`$ref`s are followed into other files, relative to the file each ref is
written in, and to URLs. Findings in other files name the file. A ref that
doesn't resolve is an `unresolved-ref` error, since every other check would
be working around the hole.

Fetched files are cached in `.legible/`, which ignores itself in git. A copy
is used without asking for as long as the server's caching headers say, then
revalidated rather than downloaded again. It covers for a server that is
down, and it is what `--offline` reads. `--refresh` revalidates everything
now, and `--no-cache` turns the cache off.

A remote file that can't be fetched, because of the network, the server,
credentials or `--offline`, is **skipped**, not failed. The report lists it,
marks the score incomplete, and sets `"complete": false` in JSON. Only a 404
or 410 counts against the spec.

## What it checks

| Category | Rules |
|---|---|
| descriptions | `operation-description`, `description-restates-name`, `parameter-description`, `property-description`, `duplicate-description` |
| naming | `operation-id`, `tool-name`, `naming-style` |
| errors | `error-responses`, `error-schema`, `error-shape` |
| examples | `request-example`, `response-example` |
| tool-schema | `unresolved-ref`, `request-body-schema`, `recursive-schema`, `argument-collision`, `schema-depth`, `parameter-count`, `untyped-property`, `polymorphism-discriminator` |
| safety | `security-declared` |

The guide is in [`rules/docs/`](rules/docs/): [how scoring works](rules/docs/index.md),
then one page per rule covering what it checks, why it trips up agents, how
to fix it, and when to ignore it. The same pages are built into the binary
as `legible explain`.

Two things the rules are built on:

- **Operations become tools.** The operationId becomes the tool's name, the
  description decides when it is chosen, and parameters and body properties
  are flattened into one argument object. Several rules
  (`tool-name`, `argument-collision`, `recursive-schema`, `schema-depth`)
  check exactly the places that conversion breaks, against the limits
  OpenAI and Anthropic publish for tool definitions.
- **The score counts passes, not just failures.** Every rule records each
  place it looked, so 3 undocumented parameters out of 200 scores far better
  than 3 out of 4. See `legible explain`.

## What it is not

- **Not a validator.** Run one as well. legible assumes the document is
  valid, and a spec can be valid and still be a bad API for agents.
- **Not a test of the live API.** Everything is read from the document.
- **Not a constraint-inventor.** No rule asks for a `pattern` or `maxLength`
  on data that has none. A constraint added to please a linter rejects valid
  input.

## Prior art

[Redocly CLI's `score`](https://redocly.com/docs/cli/commands/score) and the
[Jentic API Scorecard](https://github.com/jentic/jentic-api-scorecard) also
rate agent readiness. legible differs in being one binary with no Node,
Docker or API key that can run fully offline, in publishing exactly how each
rule decides, and in explaining each finding with a fix. It checks Stripe's
8 MB, 594-operation spec in about a third of a second. [vacuum](https://github.com/daveshanley/vacuum)
and [Spectral](https://github.com/stoplightio/spectral) are general OpenAPI
linters and pair well with it.

## Library

The engine is public, so other rulesets can be built on it:

- `spec` loads a document with line numbers and resolves `$ref`s across
  files and URLs.
- `engine` runs rules and scores the result.
- `rules` is legible's ruleset.

## License

MIT
