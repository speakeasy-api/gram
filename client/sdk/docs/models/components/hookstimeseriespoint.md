# HooksTimeSeriesPoint

A single time-series bucket for hooks activity

## Example Usage

```typescript
import { HooksTimeSeriesPoint } from "@gram/client/models/components/hookstimeseriespoint.js";

let value: HooksTimeSeriesPoint = {
  bucketStartNs: "<value>",
  eventCount: 380714,
  failureCount: 873308,
  serverName: "<value>",
  userEmail: "<value>",
};
```

## Fields

| Field                                                                 | Type                                                                  | Required                                                              | Description                                                           |
| --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- |
| `bucketStartNs`                                                       | *string*                                                              | :heavy_check_mark:                                                    | Bucket start time in Unix nanoseconds (string for JS int64 precision) |
| `eventCount`                                                          | *number*                                                              | :heavy_check_mark:                                                    | Number of events in this bucket                                       |
| `failureCount`                                                        | *number*                                                              | :heavy_check_mark:                                                    | Number of failed hook events in this bucket                           |
| `serverName`                                                          | *string*                                                              | :heavy_check_mark:                                                    | Server name                                                           |
| `userEmail`                                                           | *string*                                                              | :heavy_check_mark:                                                    | User email address                                                    |