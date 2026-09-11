# response-example

**examples** · info

Every 2xx response with a JSON body comes with an example.

## Why it trips up agents

An agent plans several calls ahead: find the customer, then list their
orders, then cancel one. To plan the second call it needs to know which
field of the first response holds the id it needs, and whether that field
is `id`, `customer_id` or `data[0].id`. An example answers that at a glance.

It is also how an agent tells a real empty result from a malformed one. If
it has seen `{"data": [], "meta": {"count": 0}}`, it can recognise an empty
page and stop.

Examples count at any level, including one on every leaf field; see
`request-example`. Responses with no body, such as `204`, are not checked.

## Fix

```yaml
responses:
  "200":
    content:
      application/json:
        schema: { $ref: "#/components/schemas/OrderPage" }
        example:
          data: [{ id: "ord_8Hk2", status: "placed", total: 2598 }]
          meta: { count: 1, next_cursor: null }
```

## When to ignore it

Operations whose response is the spec itself, or a health check that returns
`{"status": "ok"}`, gain little from one.
