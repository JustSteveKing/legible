# description-restates-name

**descriptions** · warning

A short description says something the operation's name and path do not
already say.

## Why it trips up agents

`deletePet` with the description "Delete a pet." gives a model nothing it
did not already have. It still doesn't know whether the delete can be
undone, what happens to the pet's orders, or what it gets back. A
description like that passes a "has a description" check and carries no
information.

legible flags a description of twelve words or fewer in which every word is
either part of the operationId or path, or generic API filler: "get",
"returns", "the", "details", "resource" and so on. Longer descriptions pass
even if they reuse those words, since by then they are usually saying
something.

## Fix

```yaml
# Before
delete:
  operationId: deletePet
  description: Delete a pet.

# After
delete:
  operationId: deletePet
  description: >
    Permanently removes a pet and its photos. Orders that reference the pet
    are kept, with the pet shown as deleted. This cannot be undone; to hide
    a pet from the store without losing it, use updatePet with status
    "unavailable".
```

## When to ignore it

If an operation really is fully described by its name, the fix is still a
sentence on what it returns or what it does not do. There is almost always
one worth writing.
