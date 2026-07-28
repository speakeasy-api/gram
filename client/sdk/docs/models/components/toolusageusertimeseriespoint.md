# ToolUsageUserTimeSeriesPoint

A time-series bucket for one tool usage user identity

## Example Usage

```typescript
import { ToolUsageUserTimeSeriesPoint } from "@gram/client/models/components/toolusageusertimeseriespoint.js";

let value: ToolUsageUserTimeSeriesPoint = {
  bucketStartNs: "<value>",
  eventCount: 399602,
  failureCount: 854494,
  userKey: "<value>",
  userKind: "email",
  userLabel: "<value>",
};
```

## Fields

| Field           | Type                                                                                                               | Required           | Description                                                                     |
| --------------- | ------------------------------------------------------------------------------------------------------------------ | ------------------ | ------------------------------------------------------------------------------- |
| `bucketStartNs` | _string_                                                                                                           | :heavy_check_mark: | Bucket start time in Unix nanoseconds as a string for JavaScript integer safety |
| `eventCount`    | _number_                                                                                                           | :heavy_check_mark: | Number of tool usage events in the bucket                                       |
| `failureCount`  | _number_                                                                                                           | :heavy_check_mark: | Number of failed tool usage events in the bucket                                |
| `userKey`       | _string_                                                                                                           | :heavy_check_mark: | Stable user identity value used by filters and chart grouping                   |
| `userKind`      | [components.ToolUsageUserTimeSeriesPointUserKind](../../models/components/toolusageusertimeseriespointuserkind.md) | :heavy_check_mark: | Tool usage user identity kind                                                   |
| `userLabel`     | _string_                                                                                                           | :heavy_check_mark: | User-facing label for the user identity                                         |
