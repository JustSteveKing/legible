# error-shape

**errors** · info

Error response bodies share one shape across the API.

## Why it trips up agents

An agent that has learned to read `detail` from one error will look for
`detail` in the next. If that one says `message`, and a third says
`error.description`, each failure means working the format out from
scratch, and a model under a long task will sometimes skip that and misread
the error.

legible summarises each error body by its top-level field names, and flags
the ones that differ from the most common shape. Two differently named
schemas with the same fields count as the same shape: what the agent reads
is the fields, not the component's name.

## Fix

Define one error schema, or one `components/responses` entry per status,
and reference it everywhere. RFC 9457 problem details work well for this:
well known, extensible, and with a dedicated media type,
`application/problem+json`.

## When to ignore it

When different errors carry deliberately different information, for example
validation errors with a field list next to a plain not-found. Keeping the
common fields identical and adding the extras as extension members gets you
both.
