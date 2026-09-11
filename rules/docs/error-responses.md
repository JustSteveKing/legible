# error-responses

**errors** · warning

Every operation documents at least one error response: a 4xx, a 5xx or
`default`.

## Why it trips up agents

An agent's plan survives contact with the API only if it can recover when a
call fails. That means knowing which failures are possible, whether they are
its fault (bad input, retry won't help) or transient (retry later), and what
the body will say.

An operation that documents only `200` tells the agent failure never
happens. When it does, the agent is working outside anything the spec
described, and tends either to retry pointlessly or to give up on a task it
could have fixed.

## Fix

```yaml
responses:
  "200": { $ref: "#/components/responses/Order" }
  "404": { $ref: "#/components/responses/NotFound" }
  "422": { $ref: "#/components/responses/ValidationFailed" }
  default: { $ref: "#/components/responses/Problem" }
```

Document the errors the operation actually produces, with the statuses an
agent can do something about first: 400/422 for input, 401/403 for auth, 404
for identifiers, 409 for conflicts, 429 for rate limits. A shared `default`
covers the rest.

## When to ignore it

Not recommended. Even an operation that "can't fail" can return 500.
