# Design Decisions

## 1. No pagination on GET /product

**Decision:** Return the full product list as a plain array.

**Why:**
- The OpenAPI spec defines the response as an array with no
  query parameters. A paginated envelope would break the contract.
- A menu catalog is small and changes rarely.
- Read load is better handled by caching than by pagination.

**If the catalog grows:** add optional `limit` and `cursor`
query parameters, keeping the default response unchanged.

## 2. Product ID: integer in the path, string in JSON

**Context:** The spec defines the path parameter as int64 but
`Product.id` and `productId` as strings.

**Decision:**
- Parse the path value as a positive int64.
  Not parseable, zero, negative or overflow: 400.
  Parseable but no such product: 404.
- Return `id` as a string in JSON, as the schema defines.
- Store the ID as int64 internally; convert at the API boundary.
- One shared function validates IDs for both the path and
  `productId` in order requests.

**Why:** This satisfies both parts of the spec and gives the
400/404 distinction a clear rule.

## 3. Product model and price

**Decision:** Product fields map directly to storage columns
with matching types, except `price`.

**Price:** stored as a whole number of minor units
(1250 = 12.50) and converted to a decimal only in JSON.

**Why:** Floats cannot store most decimal amounts exactly
(0.1 + 0.2 != 0.3), which causes errors once prices are
multiplied or discounted. Whole numbers are always exact.

**Extensibility:** a payment or billing API that calculates
totals is a likely next step. Storing integer minor units now
avoids float errors there and a data migration later.