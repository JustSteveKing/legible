# request-body-schema

**tool-schema** · error

Every request body has a schema, and the schema names its fields.

## Why it trips up agents

A tool's input schema is built from the operation's parameters and its
request body's schema. If the body has no schema, or is `type: object` with
nothing else, the tool takes an argument described only as "an object". The
model then has to invent every field name, and it will invent plausible
ones: `customerName` where you wanted `customer.name`, `qty` where you
wanted `quantity`. The API rejects the call or, worse, ignores the fields it
doesn't recognise and succeeds.

legible fails a body with no schema, and a body whose schema is a free-form
object: no `properties`, no composition (`allOf`/`oneOf`/`anyOf`), and
`additionalProperties` not set to `false`.

## Fix

```yaml
# Before
requestBody:
  content:
    application/json:
      schema: { type: object }

# After
requestBody:
  content:
    application/json:
      schema:
        type: object
        required: [petId, quantity]
        properties:
          petId: { type: integer, description: The pet to order. }
          quantity: { type: integer, minimum: 1 }
```

## When to ignore it

When the body really is arbitrary JSON the API stores without reading, such
as a metadata bag or a webhook payload. Say so in the description; the rule
will keep reporting it.
