# TraceSummaryRecord

Summary information for a distributed trace

## Example Usage

```typescript
import { TraceSummaryRecord } from "@gram/client/models/components";

let value: TraceSummaryRecord = {
  gramUrn: "<value>",
  logCount: 45215,
  startTimeUnixNano: 213781,
  traceId: "<id>",
};
```

## Fields

| Field                                      | Type                                       | Required                                   | Description                                |
| ------------------------------------------ | ------------------------------------------ | ------------------------------------------ | ------------------------------------------ |
| `gramUrn`                                  | *string*                                   | :heavy_check_mark:                         | Gram URN associated with this trace        |
| `httpStatusCode`                           | *number*                                   | :heavy_minus_sign:                         | HTTP status code (if applicable)           |
| `logCount`                                 | *number*                                   | :heavy_check_mark:                         | Total number of logs in this trace         |
| `startTimeUnixNano`                        | *number*                                   | :heavy_check_mark:                         | Earliest log timestamp in Unix nanoseconds |
| `traceId`                                  | *string*                                   | :heavy_check_mark:                         | Trace ID (32 hex characters)               |