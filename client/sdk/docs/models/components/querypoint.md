# QueryPoint

A single time bucket within a series

## Example Usage

```typescript
import { QueryPoint } from "@gram/client/models/components/querypoint.js";

let value: QueryPoint = {
  bucketTimeUnixNano: "<value>",
  measures: {
    cacheCreationInputTokens: 613703,
    cacheReadInputTokens: 379300,
    totalChats: 394874,
    totalCost: 5959.04,
    totalInputTokens: 272438,
    totalOutputTokens: 191330,
    totalTokens: 326513,
    totalToolCalls: 28592,
  },
};
```

## Fields

| Field                                                                | Type                                                                 | Required                                                             | Description                                                          |
| -------------------------------------------------------------------- | -------------------------------------------------------------------- | -------------------------------------------------------------------- | -------------------------------------------------------------------- |
| `bucketTimeUnixNano`                                                 | *string*                                                             | :heavy_check_mark:                                                   | Bucket start time in Unix nanoseconds (string for JS precision)      |
| `measures`                                                           | [components.QueryMeasures](../../models/components/querymeasures.md) | :heavy_check_mark:                                                   | Aggregated measure values for a group or time bucket                 |