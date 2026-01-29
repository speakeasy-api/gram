# Observability Dashboard Implementation Patterns

## Telemetry System Overview

The telemetry system uses ClickHouse for high-performance analytics with the following architecture:

### Primary Table: `telemetry_logs` (ClickHouse)

- **Schema**: `server/clickhouse/schema.sql`
- **Key columns**: `id`, `time_unix_nano`, `gram_project_id`, `gram_deployment_id`, `gram_urn`, `http_response_status_code`, `attributes` (JSON), `resource_attributes` (JSON)
- **Primary key**: `(gram_project_id, time_unix_nano, id)`
- **TTL**: 30 days

### Existing Query Patterns

See `server/internal/telemetry/repo/queries.sql.go` for reference implementations:

- Use `?` placeholders (ClickHouse style, not `$1, $2`)
- Optional filters: `(? = '' or field = ?)` - pass value twice
- Cursor pagination: Use nil UUID sentinel `00000000-0000-0000-0000-000000000000`
- JSON extraction: `JSONExtractFloat(attributes, 'key')`

### API Design

Follow Goa patterns in `server/design/telemetry/design.go`:

- RPC-style endpoints: `POST /rpc/telemetry.{methodName}`
- Input types: `{MethodName}Payload`
- Output types: `{MethodName}Result`

## Frontend Patterns

### Page Structure

Follow `client/dashboard/src/pages/logs/Logs.tsx`:

```tsx
import { Page } from "@/components/page-layout";

<Page>
  <Page.Header>
    <Page.Header.Breadcrumbs fullWidth />
  </Page.Header>
  <Page.Body fullWidth fullHeight className="!p-0">
    {/* Content */}
  </Page.Body>
</Page>;
```

### Data Fetching

Use `@tanstack/react-query` with Gram SDK:

```tsx
import { useGramContext } from "@gram/client/react-query";
import { telemetrySearchToolCalls } from "@gram/client/funcs/telemetrySearchToolCalls";
import { unwrapAsync } from "@gram/client/types/fp";

const client = useGramContext();
const { data } = useQuery({
  queryKey: ["key", params],
  queryFn: () => unwrapAsync(telemetrySearchToolCalls(client, { ... })),
});
```

### Design System

**CRITICAL**: Use Moonshine semantic colors only:

- `bg-surface-default` not `bg-white` or `bg-neutral-*`
- `border-border` not `border-gray-*`
- `text-muted-foreground` not `text-gray-*`
- `text-destructive-default` for errors
- `text-success-default` for success

### Routes

Add to `client/dashboard/src/routes.tsx` around line 237-242.

## File Locations

| Component               | Path                                              |
| ----------------------- | ------------------------------------------------- |
| ClickHouse Schema       | `server/clickhouse/schema.sql`                    |
| Query SQL               | `server/internal/telemetry/repo/queries.sql`      |
| Query Go Implementation | `server/internal/telemetry/repo/queries.sql.go`   |
| Data Models             | `server/internal/telemetry/repo/models.go`        |
| Service Implementation  | `server/internal/telemetry/impl.go`               |
| API Design              | `server/design/telemetry/design.go`               |
| Existing Logs Page      | `client/dashboard/src/pages/logs/Logs.tsx`        |
| Routes Definition       | `client/dashboard/src/routes.tsx`                 |
| Page Layout Component   | `client/dashboard/src/components/page-layout.tsx` |
