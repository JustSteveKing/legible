# untyped-property

**tool-schema** · warning

Every request body property says what kind of value it takes.

## Why it trips up agents

A property with no `type`, no `enum` or `const`, no `properties` and no
composition accepts anything, so a model sends whatever seems plausible:
`"5"` or `5`, `"true"` or `true`, `"2026-10-01"` or `1759276800`. Some of
those your API will coerce, some it will reject, and some it will store
wrong. Strict tool use also needs every property typed before it can
guarantee anything.

As with `property-description`, nested properties count for less in the
score: ½ one level down, ¼ two levels down. They are still all reported.

## Fix

```yaml
# Before
quantity: { description: How many to order }

# After
quantity: { type: integer, minimum: 1, description: How many to order }
```

In OpenAPI 3.1, a nullable field is `type: [integer, "null"]`.

## When to ignore it

When a property is deliberately open, such as a `metadata` value that can be
any JSON. Give it a description saying so. The rule will still count it,
which is honest: an open value is harder for an agent to fill.
