# ToolCallSummary

Summary information for a tool call

## Example Usage

```typescript
import { ToolCallSummary } from "@gram/client/models/components";

let value: ToolCallSummary = {
  gramUrn: "<value>",
  logCount: 994472,
  startTimeUnixNano: 251188,
  traceId: "<id>",
};
```

## Fields

| Field                                      | Type                                       | Required                                   | Description                                |
| ------------------------------------------ | ------------------------------------------ | ------------------------------------------ | ------------------------------------------ |
| `gramUrn`                                  | *string*                                   | :heavy_check_mark:                         | Gram URN associated with this tool call    |
| `httpStatusCode`                           | *number*                                   | :heavy_minus_sign:                         | HTTP status code (if applicable)           |
| `logCount`                                 | *number*                                   | :heavy_check_mark:                         | Total number of logs in this tool call     |
| `startTimeUnixNano`                        | *number*                                   | :heavy_check_mark:                         | Earliest log timestamp in Unix nanoseconds |
| `traceId`                                  | *string*                                   | :heavy_check_mark:                         | Trace ID (32 hex characters)               |