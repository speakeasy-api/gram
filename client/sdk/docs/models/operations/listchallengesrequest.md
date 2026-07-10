# ListChallengesRequest

## Example Usage

```typescript
import { ListChallengesRequest } from "@gram/client/models/operations/listchallenges.js";

let value: ListChallengesRequest = {};
```

## Fields

| Field                                                                                | Type                                                                                 | Required                                                                             | Description                                                                          |
| ------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------ |
| `outcome`                                                                            | [operations.QueryParamOutcome](../../models/operations/queryparamoutcome.md)         | :heavy_minus_sign:                                                                   | Filter by outcome.                                                                   |
| `principalUrn`                                                                       | *string*                                                                             | :heavy_minus_sign:                                                                   | Filter by principal URN.                                                             |
| `scope`                                                                              | *string*                                                                             | :heavy_minus_sign:                                                                   | Filter by scope.                                                                     |
| `projectId`                                                                          | *string*                                                                             | :heavy_minus_sign:                                                                   | Filter to a specific project.                                                        |
| `resolved`                                                                           | *boolean*                                                                            | :heavy_minus_sign:                                                                   | Filter by resolution state. True = only resolved, false = only unresolved.           |
| `ids`                                                                                | *string*[]                                                                           | :heavy_minus_sign:                                                                   | Fetch specific challenges by ID. When set, other filters and pagination are ignored. |
| `limit`                                                                              | *number*                                                                             | :heavy_minus_sign:                                                                   | Maximum number of results to return.                                                 |
| `offset`                                                                             | *number*                                                                             | :heavy_minus_sign:                                                                   | Number of results to skip.                                                           |
| `gramKey`                                                                            | *string*                                                                             | :heavy_minus_sign:                                                                   | API Key header                                                                       |
| `gramSession`                                                                        | *string*                                                                             | :heavy_minus_sign:                                                                   | Session header                                                                       |