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