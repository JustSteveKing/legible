# naming-style

**naming** · warning

operationIds, parameter names and property names each use one casing style.

## Why it trips up agents

A model that has seen `created_at` and `updated_at` will send `deleted_at`,
whether or not your API calls it `deletedAt`. Mixed styles mean an agent
has to remember every name individually rather than learn the pattern once,
and it will get some wrong.

legible doesn't prefer any style. It finds the most common style in each
group (operationIds, parameters, properties) and flags the names that
depart from it. An API that is consistently snake_case scores exactly the
same as one that is consistently camelCase.

A few names are skipped:

- single lower-case words such as `name` or `limit`, which fit every style
- header and cookie parameters, which follow HTTP convention
  (`X-Request-Id`) rather than the API's

## Fix

Rename the outliers to match. For properties and parameters that is a
breaking change for existing clients, so it may belong in the next major
version. Stop new names adding to the problem in the meantime.

## When to ignore it

When the inconsistency is imposed from outside, for example properties that
mirror a third-party payload verbatim. Consider disabling it and noting why,
rather than leaving a finding everyone learns to skip.
