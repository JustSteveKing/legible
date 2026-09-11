# request-example

**examples** · info

Every JSON request body comes with an example of a valid payload.

## Why it trips up agents

A schema says what is allowed; an example shows what is normal. Given
`amount` as an integer, a model does not know whether that is pounds or
pence until it sees `"amount": 1299` next to `"currency": "GBP"`. Examples
are also the fastest way for a model to see the overall shape of a nested
payload, where a schema has to be read piece by piece.

Anthropic's tool API accepts `input_examples` on a tool definition, and
converters fill them from the spec's examples when there are any.

An example counts wherever it sits: on the media type, on the schema, on a
nested object, or on every leaf field. The last is common in generated specs,
and documentation tools assemble a whole payload from it.

## Fix

```yaml
requestBody:
  content:
    application/json:
      schema: { $ref: "#/components/schemas/NewOrder" }
      example:
        petId: 198772
        quantity: 2
        shipDate: "2026-10-01T09:00:00Z"
```

Use realistic values. A model copies what it sees, so `"string"` and `0` as
example values are worse than none.

## When to ignore it

For trivial bodies such as `{ "enabled": true }`, where the schema already
says everything.
