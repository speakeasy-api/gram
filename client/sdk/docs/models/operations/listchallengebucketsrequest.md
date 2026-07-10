# ListChallengeBucketsRequest

## Example Usage

```typescript
import { ListChallengeBucketsRequest } from "@gram/client/models/operations/listchallengebuckets.js";

let value: ListChallengeBucketsRequest = {};
```

## Fields

| Field          | Type                                                     | Required           | Description                                                                |
| -------------- | -------------------------------------------------------- | ------------------ | -------------------------------------------------------------------------- |
| `outcome`      | [operations.Outcome](../../models/operations/outcome.md) | :heavy_minus_sign: | Filter by outcome.                                                         |
| `principalUrn` | _string_                                                 | :heavy_minus_sign: | Filter by principal URN.                                                   |
| `scope`        | _string_                                                 | :heavy_minus_sign: | Filter by scope.                                                           |
| `projectId`    | _string_                                                 | :heavy_minus_sign: | Filter to a specific project.                                              |
| `resolved`     | _boolean_                                                | :heavy_minus_sign: | Filter by resolution state. True = only resolved, false = only unresolved. |
| `limit`        | _number_                                                 | :heavy_minus_sign: | Maximum number of buckets to return.                                       |
| `offset`       | _number_                                                 | :heavy_minus_sign: | Number of buckets to skip.                                                 |
| `gramKey`      | _string_                                                 | :heavy_minus_sign: | API Key header                                                             |
| `gramSession`  | _string_                                                 | :heavy_minus_sign: | Session header                                                             |
