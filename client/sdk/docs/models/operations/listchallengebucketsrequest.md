# ListChallengeBucketsRequest

## Example Usage

```typescript
import { ListChallengeBucketsRequest } from "@gram/client/models/operations/listchallengebuckets.js";

let value: ListChallengeBucketsRequest = {};
```

## Fields

| Field                                                                      | Type                                                                       | Required                                                                   | Description                                                                |
| -------------------------------------------------------------------------- | -------------------------------------------------------------------------- | -------------------------------------------------------------------------- | -------------------------------------------------------------------------- |
| `outcome`                                                                  | [operations.Outcome](../../models/operations/outcome.md)                   | :heavy_minus_sign:                                                         | Filter by outcome.                                                         |
| `principalUrn`                                                             | *string*                                                                   | :heavy_minus_sign:                                                         | Filter by principal URN.                                                   |
| `scope`                                                                    | *string*                                                                   | :heavy_minus_sign:                                                         | Filter by scope.                                                           |
| `projectId`                                                                | *string*                                                                   | :heavy_minus_sign:                                                         | Filter to a specific project.                                              |
| `resolved`                                                                 | *boolean*                                                                  | :heavy_minus_sign:                                                         | Filter by resolution state. True = only resolved, false = only unresolved. |
| `limit`                                                                    | *number*                                                                   | :heavy_minus_sign:                                                         | Maximum number of buckets to return.                                       |
| `offset`                                                                   | *number*                                                                   | :heavy_minus_sign:                                                         | Number of buckets to skip.                                                 |
| `gramKey`                                                                  | *string*                                                                   | :heavy_minus_sign:                                                         | API Key header                                                             |
| `gramSession`                                                              | *string*                                                                   | :heavy_minus_sign:                                                         | Session header                                                             |