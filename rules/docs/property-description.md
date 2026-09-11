# property-description

**descriptions** · info

Every property of a request body schema has a description.

## Why it trips up agents

When an operation becomes a tool, request body properties become its
arguments alongside the parameters. They need explaining for the same
reasons (see `parameter-description`), but the rule is **info** rather than
warning. Request bodies tend to be built from shared component schemas, whose
property names are often self-explanatory in context (`email`, `quantity`),
and missing descriptions there cost less than on a bare query parameter.

Only request bodies are checked, since those are what an agent has to fill
in. A shared component is reported once, at its first use.

**Nested properties count for less.** A top-level property counts 1 in the
score, one level down ½, two levels down ¼, and so on; array items are a
level. Every finding is still reported. Only its share of the score changes.
A model fills a tool's top-level arguments on every call, and meets a field
three objects down far less often. Counted equally, Stripe's 10,445
undocumented nested form fields drowned out everything else.

## Fix

```yaml
Order:
  type: object
  properties:
    quantity:
      type: integer
      minimum: 1
      description: Number of units. Orders over 50 are split into several shipments.
    shipDate:
      type: string
      format: date-time
      description: >
        When to ship, in UTC. Leave it out to ship as soon as stock allows.
```

Concentrate on properties whose meaning, format or default is not obvious
from the name.

## When to ignore it

If the unflagged properties are self-evident and the flagged ones are too,
this is safe to disable. Look through the list first: it is usually a
handful that matter.
