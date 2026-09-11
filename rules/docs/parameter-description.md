# parameter-description

**descriptions** · warning

Every parameter has a description, either on the parameter or on its schema.

## Why it trips up agents

A parameter becomes a tool argument, and its description is the only
explanation the model gets for it. Without one, the model has to infer
meaning from the name alone: whether `status` takes "active" or "ACTIVE",
whether `since` is a date or a timestamp, whether `q` searches names or
everything. It will guess, confidently.

The arXiv study *Making OpenAPI Documentation Agent-Ready* (2026) found weakly
specified inputs in 88% of the endpoints it examined, and traced them
directly to agents building the wrong payloads.

A parameter declared on a path item is shared by every operation under it,
so legible reports it once, where it is declared.

## Fix

```yaml
# Before
- name: since
  in: query
  schema: { type: string }

# After
- name: since
  in: query
  description: >
    Only return orders placed on or after this date, as YYYY-MM-DD in UTC.
    Defaults to 30 days ago.
  schema: { type: string, format: date }
```

Say what the value means, what form it takes, and what happens when it is
left out.

## When to ignore it

Standard headers such as `Accept-Language` explain themselves. Everything
the API defines should be described.
