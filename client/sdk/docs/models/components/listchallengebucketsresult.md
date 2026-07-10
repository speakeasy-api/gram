# ListChallengeBucketsResult

## Example Usage

```typescript
import { ListChallengeBucketsResult } from "@gram/client/models/components/listchallengebucketsresult.js";

let value: ListChallengeBucketsResult = {
  buckets: [],
  total: 349416,
};
```

## Fields

| Field                                                                      | Type                                                                       | Required                                                                   | Description                                                                |
| -------------------------------------------------------------------------- | -------------------------------------------------------------------------- | -------------------------------------------------------------------------- | -------------------------------------------------------------------------- |
| `buckets`                                                                  | [components.ChallengeBucket](../../models/components/challengebucket.md)[] | :heavy_check_mark:                                                         | The challenge buckets.                                                     |
| `total`                                                                    | *number*                                                                   | :heavy_check_mark:                                                         | Total number of matching buckets for pagination.                           |