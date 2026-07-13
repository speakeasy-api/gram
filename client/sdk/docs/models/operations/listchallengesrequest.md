# ListChallengesRequest

## Example Usage

```typescript
import { ListChallengesRequest } from "@gram/client/models/operations/listchallenges.js";

let value: ListChallengesRequest = {};
```

## Fields

| Field          | Type                                                                         | Required           | Description                                                                          |
| -------------- | ---------------------------------------------------------------------------- | ------------------ | ------------------------------------------------------------------------------------ |
| `outcome`      | [operations.QueryParamOutcome](../../models/operations/queryparamoutcome.md) | :heavy_minus_sign: | Filter by outcome.                                                                   |
| `principalUrn` | _string_                                                                     | :heavy_minus_sign: | Filter by principal URN.                                                             |
| `scope`        | _string_                                                                     | :heavy_minus_sign: | Filter by scope.                                                                     |
| `projectId`    | _string_                                                                     | :heavy_minus_sign: | Filter to a specific project.                                                        |
| `resolved`     | _boolean_                                                                    | :heavy_minus_sign: | Filter by resolution state. True = only resolved, false = only unresolved.           |
| `ids`          | _string_[]                                                                   | :heavy_minus_sign: | Fetch specific challenges by ID. When set, other filters and pagination are ignored. |
| `limit`        | _number_                                                                     | :heavy_minus_sign: | Maximum number of results to return.                                                 |
| `offset`       | _number_                                                                     | :heavy_minus_sign: | Number of results to skip.                                                           |
| `gramKey`      | _string_                                                                     | :heavy_minus_sign: | API Key header                                                                       |
| `gramSession`  | _string_                                                                     | :heavy_minus_sign: | Session header                                                                       |
