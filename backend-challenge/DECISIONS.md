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

## 4. No stock tracking; order inputs are capped

**Decision:** No inventory or availability checks. Order
requests are bounded instead.

**Why no stock:** The spec has no stock field or out-of-stock
response.

**Input limits (configurable):**
- Max 50 lines per order
- Quantity per line: 1 to 100
- Max request body: 1 MB (413 if exceeded)

**Why:** Unbounded input costs memory and CPU and can overflow
price arithmetic.

**Duplicate lines:** the same product on several lines is
merged into one, and the merged quantity is checked against
the per-line limit.


## 5. API key authentication

**Decision:** POST /order requires an `api_key` header holding
a static key. Keys are loaded from config, each mapped to a set
of scopes.

- Missing or unknown key: 401
- Known key without the `create_order` scope: 403


**Implementation notes:**
- Keys are compared in constant time.
- Lookup sits behind a `KeyStore` interface, so config can be
  replaced by a database or auth service.
- Multiple keys are supported, which allows rotation without
  downtime.
- Keys are never logged.

## 6. Error responses

**Decision:** Every error uses the spec's `ApiResponse` shape:
- `code`: the HTTP status
- `type`: a stable machine-readable string, e.g. `invalid_coupon`
- `message`: a human-readable explanation

**Why:** It is the only error shape the spec defines. Clients
branch on `type`, never on `message`.

## 7. 400 vs 422

**Decision:**
- 400: malformed JSON, wrong types, missing required fields
- 422: well-formed request that breaks a rule (empty `items`,
  quantity out of range, unknown product, invalid coupon)
- 413: body over the size limit

**Why:** 400 means "I can't read this"; 422 means "I read it
and it's not allowed". An unknown product is 422, not 404,
because the URL exists; the problem is a value in the body.

## 8. Order input edge cases

- Empty `items` array: 422.
- `couponCode` absent, empty or whitespace-only: no coupon applied.
- Unknown JSON fields are ignored, so newer clients don't break.
- Duplicate product lines are merged (see decision 4).

## 9. Order storage and retries

**Decision:** Orders are saved through an `OrderRepository`
interface, implemented as an in-memory map guarded by a
`sync.RWMutex`.

**Why:** No GET endpoint exists, but an order that isn't kept
hasn't been placed. The interface lets a database replace the
map without touching handlers.

**Known limits:**
- Orders are lost on restart.
- A retried request creates a second order. The fix is an
  `Idempotency-Key` header: the server remembers the key and
  returns the first order on repeat. Left as a next step.

## 10. Coupon validation

**Rule:** A coupon is valid if it is 8–10 characters long and
appears in at least 2 coupon files. Both numbers are settings.

**What the data showed:**
- About 313 million codes across the 3 files.
- Only 8 codes appear in 2 or more files.
- Some codes repeat inside one file; they count as one file.
- All codes are uppercase letters and digits.

**Design:** an offline ingest tool builds the valid list; the
server loads that small list into memory.

**Ingest (`cmd/ingest`) runs in two stages:**
1. **Shard.** Each new file is fingerprinted (SHA-256), then read
   once. Every code is trimmed, uppercased, length-checked and
   written to one of 256 bucket files chosen by a stable hash
   (FNV-1a). Each file's shards are kept.
2. **Combine.** Bucket by bucket, count how many different files
   each code is in. Codes in 2 or more files are published.

**Guarantees:**
- Each file is read once, ever. Adding a file reads only that
  file; older files are counted from their shards.
- The same file added twice, even renamed, counts once.
- Memory holds one bucket at a time (about 110 MB measured),
  set by the bucket count, not by the file size.
- A crash or a bad file is never counted: files are sharded into
  a staging folder and committed by rename, manifest last.
- The valid list is replaced atomically. A failed run leaves the
  previous list live.

**Server:** loads `data/valid_coupons.txt` into an in-memory set
behind a `Store` interface, `Contains(ctx, code) (bool, error)`.
Input goes through the same normalise and length check as
ingest. Validation is a chain of checks, so future rules
(expiry, usage limits) plug in without changing the rest.

**Committed output:** the valid list is in the repo, so the
server starts in seconds without the 2 GB download.

**Scaling path (documented, not built):**
- More data: double the buckets (each bucket splits in two
  without rereading source files), or shard across machines
  with shards stored in S3.
- Bigger valid set: compact in-memory set or memory-mapped file,
  then Redis or Postgres with an in-process cache. If the store
  is down, reject coupons rather than accept them unchecked.

**Trade-offs:**
- About 3 GB of disk for shards, in exchange for low memory.
- New files are not live until ingest reruns: a new coupon may
  be briefly rejected, but an invalid one is never accepted.

**Open question:** with more files, should the rule stay
"at least 2"?

**Rule:** A coupon is valid if it is 8–10 characters long and
appears in at least 2 of the 3 coupon files.

**What the data showed:**
- About 313 million codes across the 3 files.
- Only 8 codes appear in 2 or more files.
- Some codes repeat inside the same file. These must count
  as one file, not two.
- All codes are uppercase letters and digits.

**Design: two separate steps**

1. **Ingest (offline tool, `cmd/ingest`)**
   - Reads each file line by line, never all at once.
   - Skips codes that are not 8–10 characters.
   - Splits codes into small bucket files on disk, so memory
     stays low no matter how big the files get.
   - Marks which files each code appears in, and keeps codes
     found in 2 or more files.
   - Writes the valid codes to a small file.

2. **Server**
   - Loads the valid codes into memory at startup.
   - Checks a coupon by trimming spaces, converting to
     uppercase, checking the length, then looking it up.
   - The check sits behind a `CouponValidator` interface, so
     the storage can change without touching order code.

**Why it is built this way:**
- Reading 3 GB on every order would take minutes.
- Loading all 313 million codes into memory would need over
  10 GB.
- The valid set is tiny, so a simple in-memory lookup is
  enough.
- The valid codes are committed to the repo, so the server
  starts in seconds without downloading 2 GB.

**Configurable:** the length range, the minimum number of
files, and the list of input files are settings, not
hardcoded values.

**When coupon files change:** rerun ingest and restart the
server. Until then, a new coupon may be rejected, but an
invalid coupon is never accepted.

**If valid coupons grew to millions:** move them to Postgres
with a small in-memory cache in front. If the database is
down, reject coupons rather than accept them unchecked.