# RiskTokensPoint

One UTC day of token usage split by risk involvement

## Example Usage

```typescript
import { RiskTokensPoint } from "@gram/client/models/components/risktokenspoint.js";

let value: RiskTokensPoint = {
  bucketTimeUnixNano: "<value>",
  riskyTokens: 810025,
  totalTokens: 138385,
};
```

## Fields

| Field                                                                                  | Type                                                                                   | Required                                                                               | Description                                                                            |
| -------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------- |
| `bucketTimeUnixNano`                                                                   | *string*                                                                               | :heavy_check_mark:                                                                     | Bucket start time in Unix nanoseconds (string for JS precision)                        |
| `riskyTokens`                                                                          | *number*                                                                               | :heavy_check_mark:                                                                     | Tokens from sessions with at least one active risk finding created in the query window |
| `totalTokens`                                                                          | *number*                                                                               | :heavy_check_mark:                                                                     | All session tokens in the bucket                                                       |