# SearchToolCallsPayload

Payload for searching tool call summaries

## Example Usage

```typescript
import { SearchToolCallsPayload } from "@gram/client/models/components";

let value: SearchToolCallsPayload = {
  filter: {
    from: new Date("2025-12-19T10:00:00Z"),
    to: new Date("2025-12-19T11:00:00Z"),
  },
};
```

## Fields

| Field                                                                                | Type                                                                                 | Required                                                                             | Description                                                                          |
| ------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------ |
| `cursor`                                                                             | *string*                                                                             | :heavy_minus_sign:                                                                   | Cursor for pagination                                                                |
| `filter`                                                                             | [components.SearchToolCallsFilter](../../models/components/searchtoolcallsfilter.md) | :heavy_minus_sign:                                                                   | Filter criteria for searching tool calls                                             |
| `limit`                                                                              | *number*                                                                             | :heavy_minus_sign:                                                                   | Number of items to return (1-1000)                                                   |
| `sort`                                                                               | [components.Sort](../../models/components/sort.md)                                   | :heavy_minus_sign:                                                                   | Sort order                                                                           |