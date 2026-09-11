# parameter-count

**tool-schema** · warning

No operation takes more than 15 arguments, counting parameters and
top-level request body properties together.

## Why it trips up agents

Every argument is something the model has to consider on every call. Past a
certain size, it starts dropping optional arguments it should have sent,
confusing arguments with similar names (`min_price`, `price_min`,
`minimum`), and filling in ones nobody asked for because they were there.
The tool definition also gets expensive: all of it goes into the context
window on every request.

There is no hard limit. Fifteen is roughly where this starts to show, and the
same order as the threshold Redocly's score uses for parameter hotspots.

## Fix

Options, roughly in order of preference:

- **Split by intent.** A `searchTransactions` with 29 filters is often three
  operations underneath: by place, by property, by date range.
- **Group rarely used arguments** into one structured parameter with a
  documented format.
- **Describe the common case first.** If the operation has to stay large,
  its description should say which three or four arguments matter for most
  calls.

## Options

- `max: 15`: the most arguments an operation may take before it is
  flagged.

```yaml
# .legible.yaml
rules:
  parameter-count:
    max: 30
```

To accept particular operations rather than raise the limit for all of
them, add an `ignore` with a reason. See `legible explain`.

## When to ignore it

Search and reporting endpoints over a wide dataset legitimately take many
optional filters, and every one of them may be real. If you've looked and
each argument earns its place, disable the rule. Just make sure the
description is doing the work of steering the model to the few that matter.
