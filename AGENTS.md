# AGENTS.md

Guidance for coding agents working in this repository.

`README.md` is for people running legible: what it checks, the commands, CI
use. `rules/docs/` is the rule guide, and it ships inside the binary as
`legible explain`. This file is for anyone changing the code. Don't
duplicate the other two here.

## Commands

```bash
make check                                   # gofmt + go.mod tidiness + vet + race tests; what CI runs
go test ./rules/ -run 'TestRules/tool-name'  # one rule's cases
go run ./cmd/legible check testdata/petstore.json --fail-on none
go run ./cmd/legible check rules/testdata/good.yaml   # must stay at 100 with no findings
```

Go 1.27. Dependencies are cobra and `go.yaml.in/yaml/v3`, and that is
deliberate; see below.

## Architecture

```
spec/            load a document as yaml.Nodes, resolve $refs across files and URLs, walk schemas
engine/          Rule, Context, Finding, options, suppression, scoring. Knows no rules
rules/           the agent-readiness ruleset, plus docs/ (the guide, embedded)
internal/config  .legible.yaml: find, load, apply to a ruleset, match ignores
internal/report  text, json, markdown, sarif
cmd/legible      cobra: check, rules, explain
```

`spec`, `engine` and `rules` are public on purpose. The engine is meant to be
shared with conform, a planned sibling tool that checks a spec against a
house API standard rather than for agent readiness. Keep `engine` free of
anything specific to this ruleset.

## The load-bearing decisions

**No OpenAPI library; plain `yaml.Node`s.** libopenapi was tried and dropped
on the first day. Its high-level model hides `$ref` behind lazily resolved
proxies, and its line numbers sit behind generic wrappers several layers
deep. A linter needs the opposite: every node's position, the difference
between absent and empty, and control over resolution so that recursion is
visible. JSON parses as YAML, so one parser covers both.

**A rule records passes as well as failures.** `Context.Pass`/`Fail`/`Check`.
The score is passes over checks, which is what stops a large spec being
punished for being large. A rule that only calls `Fail` still runs, but it
drags the score towards zero, since every check it records is a failure. Every
place a rule looks must go through `Check` or `Pass`. `TestRules` asserts
`checked` as well as `failed` for exactly this reason.

**Findings point at the use site, not the definition.** `SchemaNode.Site` is
the node where a schema appears, before `$ref` resolution. A problem in a
shared component is reported where an agent meets it. Rules that walk shared
components keep a `seen` set keyed by node, so a component used by twenty
operations is reported once, not twenty times.

**Recursion is detected by node identity, not `$ref` string** (`spec.Walk`).
With strings, a walk that starts at a component itself goes round the cycle
once before noticing. `TestWalkReportsRecursionAndTerminates` is the guard.

**A `$ref` resolves relative to the file it is written in** (`Document.Target`).
`#/…` in `schemas/pet.yaml` means `schemas/pet.yaml`, and `owner.yaml` there
means `schemas/owner.yaml`. The file a node came from is found through
`owner`, which records every node of every *non-root* file. The root is the
bulk of any spec, so a miss means root. Files load on first reference and
are cached with their error, so a bad ref costs one attempt. URLs are fetched
by default; that was the owner's call, against a recommendation to make them
opt-in. `Offline` stops it, and a ref skipped that way is not a finding.

**The fetch cache (`spec/cache.go`) follows the same rules as pricepaid's
download cache**, and each one is load-bearing:

- The body is written before the metadata that vouches for it, both through
  a temp file and a rename. The metadata records the body's SHA-256, and
  `Get` checks it, so a run killed mid-write leaves a miss, not a corrupt
  spec.
- A cached copy covers any failure except a 404 or 410. Serving an old copy
  of a file that has been deleted would hide exactly the breakage
  `unresolved-ref` exists to find.
- **Skipped is not failed.** Every fetch failure except 404/410 wraps
  `ErrNotFetched`. unresolved-ref ignores those; `Document.Skipped()` lists
  them with a reason; `engine.Report.Skipped` carries them into every format,
  which marks the result incomplete. A network blip in CI must not turn into
  a spec error. It must not pass silently either, which is why no format is
  allowed to leave `Skipped` out.
- `Offline` reads the cache. `Skipped` means "not fetched and not cached",
  not merely "not fetched".
- `Put` failing does not fail the run. It costs the next run a download.
- The directory writes its own `.gitignore` of `*`, so nobody has to
  remember to ignore it.
- Freshness follows the response headers (`freshness()`). A copy with
  neither validators nor caching headers gets `DefaultFreshness`, an hour;
  without that it would be downloaded in full every run, which is what the
  ONS portal did to pricepaid. A 304 renews freshness from its own headers,
  falling back to the stored validators, because a 304 need not repeat them.
  `now` is a variable so tests can move the clock; use `clock(t)`.

**Text and Markdown collapse; JSON and SARIF never do.** `collapseAfter`
findings per rule per operation, then a count. That alone was not enough
for Markdown: Stripe still came to 470 KB against GitHub's 65,536-character
comment limit. So Markdown also caps the findings table at
`maxMarkdownRows`, under a per-rule table that counts everything.
`TestMarkdownIsBounded` holds the size. Anything a machine reads must stay
complete.

**Scores are weighted; counts are not.** `CheckWeighted` changes how much a
place counts towards the score, never whether it is counted or reported.
`Checked`/`Failed` stay integers because the JSON report exposes them, while
`Score()` uses `Weight`/`FailedWeight`. Only the per-property rules use it
(`nestedWeight`: 1, ½, ¼ by depth).

**Large specs are part of the contract.** Stripe's spec (8 MB, 594
operations, 1,454 schemas) went from not finishing in five minutes to 0.3
seconds. Four things did it, and each is the obvious thing to "simplify"
back:

- `Walk` calls `fn` at every site but descends into each resolved node once
  per walk. Re-descending shared components is exponential.
- Rules that need a maximum over all routes (`schema-depth`) or a judgement
  per schema (`examples.complete`) memoise per resolved node instead of
  relying on `Walk`'s first-route depth.
- Rules that only care about names (`naming-style`) keep one seen set across
  all operations.
- Pointer lookups are cached per file.

`LEGIBLE_LARGE_SPEC=/path/to/stripe.json go test ./rules/ -run TestLargeSpec -v`
times every rule with its own deadline. Run it after touching `spec` or any
rule that walks schemas.

**Tool-name limit is 64, not 128.** OpenAI allows `{1,64}`, Anthropic
`{1,128}`; the intersection is what a spec can rely on. The same applies to
`maxDepth` (OpenAI's strict-mode limit). If you change a threshold, change
its guide page too and cite the source there. The numbers are the part
people argue with.

## Adding a rule

1. A `func(ctx *engine.Context)` in the right `rules/<category>.go`.
2. An entry in `All()`, which fixes report order.
3. `rules/docs/<id>.md`, starting `# <id>` and stating
   `**<category>** · <severity>`. `TestEveryRuleHasAGuide` checks both,
   so a severity changed in code and not in the guide fails.
4. At least one case in `rules/cases_test.go`, with `checked` and `failed`
   counted by hand. `TestRules` fails for a rule with no case.
5. Thresholds go in `Rule.Options` and are read with `ctx.Option`, never as
   a bare constant: that is what makes them configurable. The guide must
   document each as `` `name: default` ``; `TestEveryRuleHasAGuide` checks,
   and `TestOptionsChangeTheOutcome` needs a row proving the option is read.
6. Run it over both real fixtures in `testdata/`, and over Stripe, and read
   what it says. A
   rule that fires on the good fixture, or on pricepaid's deliberate choices,
   is a rule people will learn to disable.

Rules must not ask for constraints the data doesn't have: no `pattern` on
free text, no `maxLength` invented to please a linter.

## Traps

- **A schema that did not resolve belongs to unresolved-ref alone.**
  `Resolve` returns the node holding the `$ref` when it cannot follow it,
  and that node has no type, properties or examples. A rule that reads it as
  a schema reports "free-form" or "untyped" for what is really one broken or
  skipped ref, and when the ref was skipped it blames the spec for the
  network. Check `spec.Ref(n) != ""` after resolving and leave it alone.
- **Nothing in the suite touches the network.** Remote refs are tested
  against `httptest` servers. Keep it that way.
- **Descriptions can be HTML.** Stripe's are. `plain()` strips tags and
  entities before anything counts words or sentences; skip it and `<p>`
  becomes a word.
- **Config tests must not find a real `.legible.yaml`.** Discovery walks up
  to the repository root, so a config committed at the root of this repo
  would be picked up by every `cmd/legible` test. Use `--config` or
  `--no-config` in tests.

- **Cobra's `cmd.Print*` write to stderr.** Everything a caller might capture
  goes through `cmd.OutOrStdout()`.
- **Exit codes are part of the interface.** 1 is a failed gate, 2 is legible
  failing. `--fail-on-skipped` is a gate (1), not a failure (2): the spec was
  checked, just not all of it. It is the one gate that prints its reason,
  because nothing in the findings explains it. `main` maps `errGate` to 1 and everything else to 2. Do not
  return `errGate` for anything but a gate.
- **The JSON report shape is an interface.** Change it additively.
- **Minified specs have every finding on line 1.** That is correct. The
  column is what distinguishes them.
- **`hasExample` accepts leaf-level examples.** Generated specs such as
  pricepaid's put examples on fields rather than whole payloads. Tools
  assemble them, and treating that as "no example" was a bug found against
  the real fixture.
