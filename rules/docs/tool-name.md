# tool-name

**naming** · error

Every operationId is usable as a tool name, as-is, on every major tool-calling
API.

## Why it trips up agents

Tool names are constrained:

| API | Allowed |
|---|---|
| OpenAI | `^[a-zA-Z0-9_-]{1,64}$` |
| Anthropic | `^[a-zA-Z0-9_-]{1,128}$` |

legible checks the intersection: letters, digits, underscore and hyphen, at
most 64 characters. An operationId outside that, such as `pets.list`,
`get pet` or `Pets/Find`, is rejected by the API outright. Converters work
around it by rewriting the name, usually differently from one another, which
brings back all the problems of having no operationId.

## Fix

```yaml
# Before
operationId: pets.findByStatus

# After
operationId: findPetsByStatus
```

Replace dots, slashes and spaces with nothing, `_` or `-`. If a name is over
64 characters, it is usually describing the operation rather than naming it;
move the detail into the description.

## When to ignore it

When every consumer is known and all of them rename tools on the way in.
Even then, a name you control beats one a converter picks.
