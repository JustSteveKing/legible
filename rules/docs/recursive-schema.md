# recursive-schema

**tool-schema** · error

No request body schema refers back to itself.

## Why it trips up agents

Tool input schemas are usually sent to the model self-contained, with every
`$ref` inlined. A recursive schema, such as a `Category` with
`children: [Category]` or a `Filter` with `and: [Filter]`, cannot be inlined:
it never ends. Converters either give up, truncate at an arbitrary depth, or
drop the recursive field, and each of those produces a tool that is not
your operation.

Structured and strict tool use make this a hard failure. Anthropic's docs
list recursive schemas as unsupported for strict tool use, so the request is
rejected before the model sees it.

legible reports the chain of `$ref`s that closes the loop, so you can see
where to cut it.

## Fix

Bound the recursion explicitly. Most real payloads are only a level or two
deep:

```yaml
# Before
Filter:
  properties:
    field: { type: string }
    and: { type: array, items: { $ref: "#/components/schemas/Filter" } }

# After: one level of nesting, spelled out
Filter:
  properties:
    field: { type: string }
    and: { type: array, items: { $ref: "#/components/schemas/LeafFilter" } }
LeafFilter:
  properties:
    field: { type: string }
```

Or accept the nested part as a string in a documented format, such as a
query expression, and parse it on the server.

## When to ignore it

When no consumer uses the operation as a tool. Response schemas are not
checked, because recursion there only makes a response harder to read.
