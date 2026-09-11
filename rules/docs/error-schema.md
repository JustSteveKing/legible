# error-schema

**errors** · warning

Every documented error response has a body schema.

## Why it trips up agents

A status code tells an agent that something failed. The body tells it what
to do next: which field was invalid, which id was not found, how long to
wait before retrying. An error response documented as just
`"400": { description: Invalid input }` gives the agent a code and a word,
and it has to guess the rest.

Errors are where agents most need structured information and where specs
most often have none. [RFC 9457](https://www.rfc-editor.org/rfc/rfc9457)
problem details are a good default: `type`, `title`, `status`, `detail`,
and extension members for field-level errors.

A shared `components/responses` entry is reported once, however many
operations use it.

## Fix

```yaml
components:
  responses:
    ValidationFailed:
      description: The request was well formed but failed validation.
      content:
        application/problem+json:
          schema: { $ref: "#/components/schemas/Problem" }
```

## When to ignore it

For statuses that truly have no body, such as a `304`. Those are not error
codes and are not checked. Every 4xx and 5xx your API returns has something
worth saying.
