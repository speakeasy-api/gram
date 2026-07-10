# QueryMeasures

Aggregated measure values for a group or time bucket

## Example Usage

```typescript
import { QueryMeasures } from "@gram/client/models/components/querymeasures.js";

let value: QueryMeasures = {
  cacheCreationInputTokens: 369204,
  cacheReadInputTokens: 964953,
  totalChats: 315794,
  totalCost: 5594.57,
  totalInputTokens: 708906,
  totalOutputTokens: 804437,
  totalTokens: 424405,
  totalToolCalls: 328794,
};
```

## Fields

| Field                              | Type                               | Required                           | Description                        |
| ---------------------------------- | ---------------------------------- | ---------------------------------- | ---------------------------------- |
| `cacheCreationInputTokens`         | *number*                           | :heavy_check_mark:                 | Sum of cache creation input tokens |
| `cacheReadInputTokens`             | *number*                           | :heavy_check_mark:                 | Sum of cache read input tokens     |
| `totalChats`                       | *number*                           | :heavy_check_mark:                 | Number of distinct chat sessions   |
| `totalCost`                        | *number*                           | :heavy_check_mark:                 | Total cost in USD                  |
| `totalInputTokens`                 | *number*                           | :heavy_check_mark:                 | Sum of input tokens                |
| `totalOutputTokens`                | *number*                           | :heavy_check_mark:                 | Sum of output tokens               |
| `totalTokens`                      | *number*                           | :heavy_check_mark:                 | Sum of all tokens                  |
| `totalToolCalls`                   | *number*                           | :heavy_check_mark:                 | Total number of tool calls         |