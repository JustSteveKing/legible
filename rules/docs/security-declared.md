# security-declared

**safety** · warning

Every operation states what authentication it needs, either on itself or
through the document's top-level `security`.

## Why it trips up agents

An agent has to know, before it calls an operation, whether it needs
credentials and which ones. An operation with no security requirement could
mean it is public, that the author forgot, or that auth is described in
prose somewhere. The agent can't tell which. It will either call without
credentials and fail, or send credentials to an operation that doesn't want
them.

The arXiv study *Making OpenAPI Documentation Agent-Ready* (2026) found
missing or unclear auth in 68% of the endpoints it examined, and it was one
of the three problems that most directly caused agents to fail tasks.

An explicit `security: []` passes: it says "no auth needed" on purpose, which
is exactly what an agent needs to know.

## Fix

```yaml
# Everything needs a bearer token...
security:
  - bearerAuth: []

paths:
  /health:
    get:
      security: []   # ...except this, deliberately
```

Define the schemes under `components.securitySchemes`, and say in their
descriptions where a caller gets a credential.

## When to ignore it

Don't. Even a fully public API should say so once at the top level with
`security: []`, which makes the rule pass everywhere.
