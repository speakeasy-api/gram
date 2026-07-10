# ClaudeTurnUsage

## Example Usage

```typescript
import { ClaudeTurnUsage } from "@gram/client/models/components/claudeturnusage.js";

let value: ClaudeTurnUsage = {
  cacheCreationTokens: 401937,
  cacheReadTokens: 105552,
  costMicros: 670185,
  costUsd: 2515.94,
  endTimeUnixNano: "<value>",
  inputTokens: 400218,
  models: [],
  outputTokens: 682879,
  promptId: "<id>",
  querySources: [],
  requestCount: 473109,
  startTimeUnixNano: "<value>",
  totalTokens: 705759,
};
```

## Fields

| Field                                                          | Type                                                           | Required                                                       | Description                                                    |
| -------------------------------------------------------------- | -------------------------------------------------------------- | -------------------------------------------------------------- | -------------------------------------------------------------- |
| `cacheCreationTokens`                                          | *number*                                                       | :heavy_check_mark:                                             | Cache creation tokens used by this turn.                       |
| `cacheReadTokens`                                              | *number*                                                       | :heavy_check_mark:                                             | Cache read tokens used by this turn.                           |
| `costMicros`                                                   | *number*                                                       | :heavy_check_mark:                                             | Total cost for this turn in micros of a USD.                   |
| `costUsd`                                                      | *number*                                                       | :heavy_check_mark:                                             | Total USD cost for this turn.                                  |
| `endTimeUnixNano`                                              | *string*                                                       | :heavy_check_mark:                                             | Latest OTEL log timestamp in this turn, as Unix nanoseconds.   |
| `inputTokens`                                                  | *number*                                                       | :heavy_check_mark:                                             | Input tokens used by this turn.                                |
| `models`                                                       | *string*[]                                                     | :heavy_check_mark:                                             | Distinct model names used by this turn.                        |
| `outputTokens`                                                 | *number*                                                       | :heavy_check_mark:                                             | Output tokens used by this turn.                               |
| `promptId`                                                     | *string*                                                       | :heavy_check_mark:                                             | Claude prompt.id that correlates events for one user turn.     |
| `querySources`                                                 | *string*[]                                                     | :heavy_check_mark:                                             | Distinct Claude query sources used by this turn.               |
| `requestCount`                                                 | *number*                                                       | :heavy_check_mark:                                             | Number of Claude API request events in this turn.              |
| `startTimeUnixNano`                                            | *string*                                                       | :heavy_check_mark:                                             | Earliest OTEL log timestamp in this turn, as Unix nanoseconds. |
| `totalTokens`                                                  | *number*                                                       | :heavy_check_mark:                                             | Total tokens used by this turn.                                |