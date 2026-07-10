# ListRiskResultsResult

## Example Usage

```typescript
import { ListRiskResultsResult } from "@gram/client/models/components/listriskresultsresult.js";

let value: ListRiskResultsResult = {
  results: [],
  totalCount: 610370,
};
```

## Fields

| Field        | Type                                                             | Required           | Description                                           |
| ------------ | ---------------------------------------------------------------- | ------------------ | ----------------------------------------------------- |
| `nextCursor` | _string_                                                         | :heavy_minus_sign: | Cursor for the next page of results.                  |
| `results`    | [components.RiskResult](../../models/components/riskresult.md)[] | :heavy_check_mark: | The list of risk results.                             |
| `totalCount` | _number_                                                         | :heavy_check_mark: | Total number of findings across all enabled policies. |
