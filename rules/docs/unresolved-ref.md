# unresolved-ref

**tool-schema** · error

Every `$ref` an agent could reach resolves to something.

## Why it trips up agents

A `$ref` that points at nothing means the schema isn't there. A tool built
from the operation is missing that part of its input or output, and a
converter will either fail outright or quietly replace it with "any value".
Every other rule is also checking around the hole: a missing request schema
can't be found to lack descriptions.

legible follows refs the way OpenAPI defines them:

- `#/components/schemas/Pet` resolves in the file the ref is written in.
  That is not necessarily the root document.
- `schemas/pet.yaml#/Pet` resolves relative to the directory of the file
  the ref is written in.
- `https://example.com/common.yaml#/Error` is fetched, and cached in
  `.legible/`.

A remote ref is only a finding when the server says the file doesn't
exist, i.e. a 404 or 410. Every other way a fetch can fail is legible not
getting the file, not the spec being wrong:

- running `--offline` with no cached copy
- a network error or timeout
- a 5xx
- a 401 or 403 from a private host
- a 429

Those refs are **skipped**. They aren't counted either way, each report
lists them with the reason, the score is marked incomplete, and JSON sets
`"complete": false`. A cached copy from an earlier run stands in where there
is one. To fail the run when anything was skipped, use `--fail-on-skipped`.

Only refs reachable from the root document are checked. An unused file in
the same directory is not.

## Fix

The finding names the ref and why it failed: a missing file, a file that
isn't valid YAML or JSON, a pointer to a key that doesn't exist, or a URL
that didn't answer. The most common cause is a file moved without updating
the refs to it. Remember they resolve from the referring file's directory,
not the root document's.

## When to ignore it

When the broken ref is in a file you don't own, such as a shared schema
vendored from upstream, ignore it by file rather than turning the rule off:

```yaml
ignore:
  - rule: unresolved-ref
    files: ["schemas/vendor/**"]
    reason: Vendored from upstream; reported to them.
```

Otherwise, fix or remove the ref.
