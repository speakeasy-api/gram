# SkillTimeSeriesPoint

A single time-series bucket for skill usage activity

## Example Usage

```typescript
import { SkillTimeSeriesPoint } from "@gram/client/models/components/skilltimeseriespoint.js";

let value: SkillTimeSeriesPoint = {
  bucketStartNs: "<value>",
  eventCount: 433814,
  skillName: "<value>",
};
```

## Fields

| Field                                                                 | Type                                                                  | Required                                                              | Description                                                           |
| --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- |
| `bucketStartNs`                                                       | *string*                                                              | :heavy_check_mark:                                                    | Bucket start time in Unix nanoseconds (string for JS int64 precision) |
| `eventCount`                                                          | *number*                                                              | :heavy_check_mark:                                                    | Number of skill use events in this bucket                             |
| `skillName`                                                           | *string*                                                              | :heavy_check_mark:                                                    | Skill name                                                            |