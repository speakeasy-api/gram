# QueryResult

Result of a generic analytics query: a grouped table and a matching per-group timeseries over the same data slice.

## Example Usage

```typescript
import { QueryResult } from "@gram/client/models/components/queryresult.js";

let value: QueryResult = {
  groupBy: "<value>",
  intervalSeconds: 829871,
  table: [
    {
      dimensionValues: {
        "key": [
          "<value 1>",
          "<value 2>",
        ],
      },
      groupValue: "<value>",
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
    },
  ],
  timeseries: [],
};
```

## Fields

| Field                                                                   | Type                                                                    | Required                                                                | Description                                                             |
| ----------------------------------------------------------------------- | ----------------------------------------------------------------------- | ----------------------------------------------------------------------- | ----------------------------------------------------------------------- |
| `groupBy`                                                               | *string*                                                                | :heavy_check_mark:                                                      | Echoes the requested group_by dimension; empty when none was requested. |
| `intervalSeconds`                                                       | *number*                                                                | :heavy_check_mark:                                                      | The timeseries bucket interval in seconds.                              |
| `table`                                                                 | [components.QueryRow](../../models/components/queryrow.md)[]            | :heavy_check_mark:                                                      | Grouped totals over the full time range, ordered by sort_by descending. |
| `timeseries`                                                            | [components.QuerySeries](../../models/components/queryseries.md)[]      | :heavy_check_mark:                                                      | One series per group value (aligned with table rows), each gap-filled.  |