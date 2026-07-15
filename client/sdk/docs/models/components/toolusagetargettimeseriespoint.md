# ToolUsageTargetTimeSeriesPoint

A time-series bucket for one tool usage target

## Example Usage

```typescript
import { ToolUsageTargetTimeSeriesPoint } from "@gram/client/models/components/toolusagetargettimeseriespoint.js";

let value: ToolUsageTargetTimeSeriesPoint = {
  bucketStartNs: "<value>",
  eventCount: 318904,
  failureCount: 11119,
  targetId: "<id>",
  targetKind: "server",
  targetLabel: "<value>",
  targetType: "skill",
};
```

## Fields

| Field           | Type                                                           | Required           | Description                                                                     |
| --------------- | -------------------------------------------------------------- | ------------------ | ------------------------------------------------------------------------------- |
| `bucketStartNs` | _string_                                                       | :heavy_check_mark: | Bucket start time in Unix nanoseconds as a string for JavaScript integer safety |
| `eventCount`    | _number_                                                       | :heavy_check_mark: | Number of tool usage events in the bucket                                       |
| `failureCount`  | _number_                                                       | :heavy_check_mark: | Number of failed tool usage events in the bucket                                |
| `targetId`      | _string_                                                       | :heavy_check_mark: | Stable target identifier used by filters and chart grouping                     |
| `targetKind`    | [components.TargetKind](../../models/components/targetkind.md) | :heavy_check_mark: | Tool usage aggregation target kind                                              |
| `targetLabel`   | _string_                                                       | :heavy_check_mark: | User-facing label for the target                                                |
| `targetType`    | [components.TargetType](../../models/components/targettype.md) | :heavy_check_mark: | Tool usage target type                                                          |
