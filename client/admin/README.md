# Admin SDK usage

The Admin application uses the private, same-origin clients in
[`src/lib/gramAdminClient.ts`](src/lib/gramAdminClient.ts). Keep generated SDK
imports and request options inside that boundary rather than exposing them to
page components.

## Organization meter usage

`/organizations/:org-id-or-slug/billing` reports organization totals for storage
(tokens under management), MCP bandwidth (bytes), and risk scans (tokens).
It uses the same ordinary meter readings and billing-cycle boundaries as the
customer dashboard, without facet queries or breakdown controls.

The admin process requires both the primary ClickHouse connection settings
(`CLICKHOUSE_*`) and the read-replica settings (`CLICKHOUSE_READ_*`), just like
the main app. PAYG billing operations use the primary connection; meter usage
reports use the read replica. Startup fails if either connection cannot be
established. Local development can point both connections at the same instance.

Product, interval, cumulative mode, and custom dates are URL search parameters.
Daily, Monday-start weekly, and calendar-month totals use UTC. Date ranges
include both displayed dates and cannot exceed three calendar months.
Cumulative charts stop at retrieval time rather than projecting future usage.
Period totals and rollups retain exact integer precision; only chart coordinates
are converted to JavaScript numbers. These usage reports are not invoice estimates.

The existing demo seed supplies ordinary readings for all three products.
No additional seed or schema migration is required.

## Image upload mutation variables

The generated `useAdminUploadPlatformImageMutation` hook takes a variables
object, **not a bare Blob**. Inside the SDK integration layer, given `mutate`
from that hook and a browser `File` or `Blob`, use:

```ts
mutate({ request: blob });
```

For example, a browser file-input handler can submit the selected file without
Node.js filesystem APIs or an asynchronous form handler:

```ts
const file = input.files?.[0];
if (file) {
  mutate({ request: file });
}
```

`File` extends `Blob`. The generated
[`AdminUploadPlatformImageMutationVariables`](src/sdk/src/react-query/adminUploadPlatformImage.ts)
type also accepts `ArrayBuffer`, `Uint8Array`, and `ReadableStream<Uint8Array>`
under `request`; `options` is optional and remains internal to the integration
layer.

### Generated documentation limitation

The mutation example in [`src/sdk/REACT_QUERY.md`](src/sdk/REACT_QUERY.md)
currently passes a bare Blob and uses `await openAsBlob(...)` inside a
non-async browser form handler. Use the wrapped browser example above instead.

The pinned Speakeasy generator (`1.796.1`) owns that file. Its bundled
`sdk-customization/readme-customization.md` guide documents custom sections in
`README.md` and `generation.additionalDocs`, but does not document a source
override for the React Query mutation template. This non-generated guide keeps
the correction durable without modifying generated output or enabling
persistent edits. The generated example still needs an upstream template fix.

## Organization list API filters

`GET /admin/organizations.list` accepts these optional query parameters. All
filters intersect with search, account type and trial state; `total` counts the
full filtered set independently of cursor, page, offset or limit.

- `min_members` / `max_members`: inclusive nonnegative integer bounds on active
  member count, including zero. Each can be omitted. Values must fit signed
  int64 (`0`–`9223372036854775807`), and minimum cannot exceed maximum.
  The handwritten `listOrganizations` API client accepts safe JavaScript numbers
  or decimal strings; use strings above `Number.MAX_SAFE_INTEGER` to avoid
  precision loss. The generated admin SDK instead accepts native `bigint` and
  serializes it losslessly to decimal query strings.
- `created_from` / `created_to`: strict `YYYY-MM-DD` UTC calendar dates, **both
  inclusive**, each independently optional. The service converts the start to
  UTC midnight and the end to an exclusive bound at the next day's UTC midnight,
  including the entire end day at database precision. Invalid calendar dates or
  a reversed range are rejected.
- `disabled_only`: omission preserves `include_disabled` / `disabled_states`
  behavior, including the legacy exact organization/WorkOS-ID exception.
  Explicit `true` restricts results to disabled organizations even on an exact
  ID search; explicit `false` does not restrict status. Either explicit value
  overrides **both** legacy status parameters when they conflict. Other filters
  continue to apply to exact-ID searches.

Invalid bounds, fractional or overflowing member counts, and invalid dates
return a 4xx response. These are API parameters only; no UI filter controls or
preset-to-date mappings are introduced here.

### Bigint React Query keys

The generated normal and infinite organization-list key factories normalize only
`minMembers` and `maxMembers` to lossless decimal strings. Request payloads remain
native `bigint`; HTTP serialization is unchanged. This keeps the keys compatible
with TanStack Query's default JSON hashing without narrowing the int64 range.

The two-factory fix is maintained in the admin SDK's
[Speakeasy native patch](src/sdk/.speakeasy/patches/src/react-query/adminListOrganizations.core.ts.patch),
not by editing generated files or adding a custom post-generation script.
`mise run gen:sdk` applies the patch during generation. If the generator changes
these factories, update the patch and run the real QueryClient regression tests
in `src/lib/gramAdminApi.test.ts`; missing targets can otherwise be reported only
as generator warnings. See [Speakeasy patch files](https://www.speakeasy.com/docs/sdks/customize/code/patch-files/patch-files).
