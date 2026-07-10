# QuerySeries

A gap-filled timeseries for a single group value (one line on the chart).

## Example Usage

```typescript
import { QuerySeries } from "@gram/client/models/components/queryseries.js";

let value: QuerySeries = {
  groupValue: "<value>",
  points: [],
};
```

## Fields

| Field                                                                                                                               | Type                                                                                                                                | Required                                                                                                                            | Description                                                                                                                         |
| ----------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------- |
| `groupValue`                                                                                                                        | *string*                                                                                                                            | :heavy_check_mark:                                                                                                                  | The dimension value for this series. Empty string when no group_by was requested; 'Other' for the rolled-up remainder beyond top_n. |
| `points`                                                                                                                            | [components.QueryPoint](../../models/components/querypoint.md)[]                                                                    | :heavy_check_mark:                                                                                                                  | Time buckets in ascending order, gap-filled with zeros.                                                                             |