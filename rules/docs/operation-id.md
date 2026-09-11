# operation-id

**naming** · error

Every operation has an `operationId`, and no two share one.

## Why it trips up agents

The operationId is what every OpenAPI-to-tool converter, MCP bridge and SDK
generator uses as the tool's name. Without one, each tool invents a name from
the method and path, and each invents a different one: `get_pets_petId`,
`getPetsByPetId`, `GET_/pets/{petId}`. The last isn't even a valid tool name.
Rename a path segment and every agent prompt, eval and cached plan that
referred to the old name breaks.

Duplicates are worse. Two tools cannot share a name, so a converter either
drops one or suffixes it arbitrarily, and the agent sees a tool that isn't
the operation you meant.

## Fix

```yaml
paths:
  /pets/{petId}:
    get:
      operationId: getPet
```

Choose a verb and a noun, keep them stable once published, and use one
casing style across the API (see `naming-style`).

## When to ignore it

Don't. An operationId costs one line and is the most important identifier
an agent sees.
