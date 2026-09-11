# schema-depth

**tool-schema** · warning

No request body schema nests objects or arrays more than five levels deep.

## Why it trips up agents

Every level of nesting is a place for a model to put a field in the wrong
object. Deep payloads are where tool calls most often come back
structurally wrong, with the right values at the wrong depth.

There is a hard limit as well: OpenAI's strict mode rejects schemas nested
more than five levels deep, which is the threshold legible uses.

Depth counts objects and arrays. Composition (`allOf`, `oneOf`, `anyOf`)
does not add a level: `allOf` of two flat objects is still flat to whoever
fills it in.

## Fix

Flatten what can be flattened. Wrappers that exist only for grouping, such
as `{ "shipping": { "address": { "location": { ... } } } }`, can usually
lose a level or two. Payloads that really are deep can often be split into
operations that build the structure a step at a time: create the order, then
add lines to it.

## Options

- `max: 5`: the deepest nesting allowed. Lowering it is reasonable if your
  agents use a stricter tool API; raising it past 5 means some strict-mode
  consumers will reject the schema.

## When to ignore it

When the depth mirrors a domain model an agent will never write by hand, for
example a document format submitted as a whole. Check the finding points at
the part you expected.
