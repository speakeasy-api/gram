# QueryRow

One row of the grouped table: measures aggregated over the full time range for a single group value.

## Example Usage

```typescript
import { QueryRow } from "@gram/client/models/components/queryrow.js";

let value: QueryRow = {
  dimensionValues: {
    key: ["<value 1>", "<value 2>", "<value 3>"],
    key1: [],
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
};
```

## Fields

| Field             | Type                                                                 | Required           | Description                                                                                                                                                                                                                                                                                                                              |
| ----------------- | -------------------------------------------------------------------- | ------------------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `dimensionValues` | Record<string, _string_[]>                                           | :heavy_check_mark: | Distinct values of every allowlisted dimension other than the group_by dimension, observed within this group. Keyed by dimension identifier (the same keys used for group_by/filters, e.g. when grouping by department_name: 'email' -> [...], 'job_title' -> [...], 'role' -> [...]). Empty values are omitted and each list is capped. |
| `groupValue`      | _string_                                                             | :heavy_check_mark: | The dimension value for this row. Empty string when no group_by was requested; 'Other' for the rolled-up remainder beyond top_n.                                                                                                                                                                                                         |
| `measures`        | [components.QueryMeasures](../../models/components/querymeasures.md) | :heavy_check_mark: | Aggregated measure values for a group or time bucket                                                                                                                                                                                                                                                                                     |
