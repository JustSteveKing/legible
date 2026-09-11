# operation-description

**descriptions** · warning

Every operation has a `description`, and it runs to more than one sentence.

## Why it trips up agents

A model picks which tool to call almost entirely from its description. The
operationId says what it is called, the parameters say what it takes, and
the description is the only place that says *when to use it*: what it does,
what it returns, and how it differs from the operation next to it.

Anthropic's tool-definition guidance calls detailed descriptions "by far the
most important factor in tool performance" and asks for at least three or
four sentences each. A `summary` doesn't substitute for one. Summaries are
titles, written to fit in a sidebar.

legible fails an operation with no description or summary, with only a
summary, or with a one-sentence description.

## Fix

```yaml
# Before
get:
  operationId: listOrders
  summary: List orders

# After
get:
  operationId: listOrders
  summary: List orders
  description: >
    Lists the authenticated customer's orders, newest first, 20 per page.
    Use this to find an order when you do not have its id; use getOrder
    when you do. Cancelled orders are included and carry status
    "cancelled". Line items are not included, only totals.
```

Cover what it does, when to choose it over a similar operation, and what the
response does and does not contain.

## Options

- `min-sentences: 2`: the shortest description that passes. HTML and entities
  are stripped before counting, so `<p>One.</p><p>Two.</p>` is two
  sentences.

## When to ignore it

Rarely. Health checks and similar plumbing that no agent will call can get a
one-line description. It is usually simpler to write the second sentence
than to turn the rule off.
