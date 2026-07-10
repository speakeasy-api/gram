# ListHooksTracesPayload

Payload for listing hook traces

## Example Usage

```typescript
import { ListHooksTracesPayload } from "@gram/client/models/components/listhookstracespayload.js";

let value: ListHooksTracesPayload = {
  filters: [
    {
      path: "@user.region",
    },
  ],
  from: new Date("2025-12-19T10:00:00Z"),
  to: new Date("2025-12-19T11:00:00Z"),
  typesToInclude: [
    "mcp",
    "skill",
  ],
};
```

## Fields

| Field                                                                                                                | Type                                                                                                                 | Required                                                                                                             | Description                                                                                                          | Example                                                                                                              |
| -------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------- |
| `cursor`                                                                                                             | *string*                                                                                                             | :heavy_minus_sign:                                                                                                   | Cursor for pagination (trace_id)                                                                                     |                                                                                                                      |
| `filters`                                                                                                            | [components.LogFilter](../../models/components/logfilter.md)[]                                                       | :heavy_minus_sign:                                                                                                   | Filter conditions for the search query                                                                               |                                                                                                                      |
| `from`                                                                                                               | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date)                        | :heavy_check_mark:                                                                                                   | Start time in ISO 8601 format (e.g., '2025-12-19T10:00:00Z')                                                         | 2025-12-19T10:00:00Z                                                                                                 |
| `limit`                                                                                                              | *number*                                                                                                             | :heavy_minus_sign:                                                                                                   | Number of items to return (1-1000)                                                                                   |                                                                                                                      |
| `sort`                                                                                                               | [components.Sort](../../models/components/sort.md)                                                                   | :heavy_minus_sign:                                                                                                   | Sort order                                                                                                           |                                                                                                                      |
| `to`                                                                                                                 | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date)                        | :heavy_check_mark:                                                                                                   | End time in ISO 8601 format (e.g., '2025-12-19T11:00:00Z')                                                           | 2025-12-19T11:00:00Z                                                                                                 |
| `typesToInclude`                                                                                                     | [components.ListHooksTracesPayloadTypesToInclude](../../models/components/listhookstracespayloadtypestoinclude.md)[] | :heavy_minus_sign:                                                                                                   | Hook types to include (mcp, local, skill). If empty or not provided, includes all types.                             | [<br/>"mcp",<br/>"skill"<br/>]                                                                                       |