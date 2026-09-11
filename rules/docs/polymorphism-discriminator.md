# polymorphism-discriminator

**tool-schema** · warning

Every `oneOf` or `anyOf` in a request body with more than one real variant
has a `discriminator`.

## Why it trips up agents

A union means the model has to decide which variant it is building, and then
build that variant and not a blend of two. Without a discriminator, the
only way to tell variants apart is to compare their property lists, and
models regularly produce a payload with half the fields of each. Most
converters also drop the union down to its first variant, or to "any
object", which loses the choice altogether.

A discriminator names the property that decides the variant, such as
`type: card` or `type: bank_transfer`. That gives the model one field to
choose first and a clear set of fields that follow from it.

Only object variants count. `anyOf: [X, {type: "null"}]` is how OpenAPI 3.1
spells "nullable", and a union of scalars is decided by the value itself.
Stripe's `anyOf: [string, enum: [""]]`, meaning "a value, or empty to clear
it", has nothing for a discriminator to point at.

## Fix

```yaml
PaymentMethod:
  oneOf:
    - $ref: "#/components/schemas/Card"
    - $ref: "#/components/schemas/BankTransfer"
  discriminator:
    propertyName: type
    mapping:
      card: "#/components/schemas/Card"
      bank_transfer: "#/components/schemas/BankTransfer"
```

Each variant should declare `type` as a required property with a single
`enum` or `const` value.

## When to ignore it

When the variants cannot be confused because their required fields do not
overlap at all. A discriminator is still cheaper than relying on that.
