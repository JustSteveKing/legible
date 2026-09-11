# argument-collision

**tool-schema** · error

An operation's parameters and top-level request body properties can be
merged into one argument object without two of them sharing a name.

## Why it trips up agents

A tool takes a single JSON object of arguments. To expose an HTTP operation
as a tool, a converter flattens its path, query, header and cookie
parameters and its request body's top-level properties into that one object.
If `username` is both a path parameter and a body property, the flattened
tool has one `username` argument for two different things. The converter
either renames one (`body_username`, which the model has never seen
documented), nests the body under a `body` key (which few models expect), or
silently lets one win.

`PUT /user/{username}` with a `User` body that also has a `username` is the
classic case. The model sends one value and the API receives it in the wrong
place, or twice.

## Fix

Rename one side so each argument means one thing:

```yaml
/users/{userId}:
  put:
    parameters:
      - { name: userId, in: path, required: true, schema: { type: integer } }
    requestBody:
      content:
        application/json:
          schema: { $ref: "#/components/schemas/User" }  # has `username`, not `userId`
```

Or leave the identifying field out of the update schema, since it is already
in the path.

## When to ignore it

When both sides must always carry the same value and the server rejects a
mismatch. That still costs every converter a special case, so make sure the
description says so.
