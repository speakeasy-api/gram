# SearchToolCallsResult

Result of searching tool call summaries

## Example Usage

```typescript
import { SearchToolCallsResult } from "@gram/client/models/components";

let value: SearchToolCallsResult = {
  enabled: true,
  toolCalls: [
    {
      gramUrn: "<value>",
      logCount: 311718,
      startTimeUnixNano: 688132,
      traceId: "<id>",
    },
  ],
};
```

## Fields

| Field                                                                      | Type                                                                       | Required                                                                   | Description                                                                |
| -------------------------------------------------------------------------- | -------------------------------------------------------------------------- | -------------------------------------------------------------------------- | -------------------------------------------------------------------------- |
| `enabled`                                                                  | *boolean*                                                                  | :heavy_check_mark:                                                         | Whether tool metrics are enabled for the organization                      |
| `nextCursor`                                                               | *string*                                                                   | :heavy_minus_sign:                                                         | Cursor for next page                                                       |
| `toolCalls`                                                                | [components.ToolCallSummary](../../models/components/toolcallsummary.md)[] | :heavy_check_mark:                                                         | List of tool call summaries                                                |