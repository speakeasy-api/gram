# QueryRiskTokensResult

Result of the token-by-risk breakdown query

## Example Usage

```typescript
import { QueryRiskTokensResult } from "@gram/client/models/components/queryrisktokensresult.js";

let value: QueryRiskTokensResult = {
  intervalSeconds: 90263,
  points: [
    {
      bucketTimeUnixNano: "<value>",
      riskyTokens: 236878,
      totalTokens: 792607,
    },
  ],
};
```

## Fields

| Field                                                                                      | Type                                                                                       | Required                                                                                   | Description                                                                                |
| ------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------ |
| `intervalSeconds`                                                                          | *number*                                                                                   | :heavy_check_mark:                                                                         | Timeseries bucket width in seconds. Always 86400 — the source aggregate is bucketed daily. |
| `points`                                                                                   | [components.RiskTokensPoint](../../models/components/risktokenspoint.md)[]                 | :heavy_check_mark:                                                                         | Gap-filled daily buckets in ascending time order                                           |