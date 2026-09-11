# duplicate-description

**descriptions** · warning

No two operations share the same description.

## Why it trips up agents

When a model chooses between tools, descriptions are what it compares. Two
tools described identically are, to the model, the same tool with two
names, and it will pick between them close to arbitrarily.

This usually comes from copying a description that is really a note about
auth ("This can only be done by the logged in user.") into every operation
it applies to. The note is true, but it doesn't describe the operation.

Comparison ignores case and punctuation.

## Fix

Describe what each operation does, and move shared caveats somewhere that
applies to all of them: a security requirement for auth, or a sentence
added after the operation-specific text.

```yaml
# Before: on createUser, updateUser and deleteUser alike
description: This can only be done by the logged in user.

# After
post:
  operationId: createUser
  description: >
    Creates a user account and returns it with its assigned id. The
    username must be unique; a taken username returns 409.
  security: [{ session: [] }]
```

## When to ignore it

Never for operations an agent can call. If two operations really do the same
thing, one of them probably should not be exposed.
