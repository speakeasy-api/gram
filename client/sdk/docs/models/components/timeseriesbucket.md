# TimeSeriesBucket

A single time bucket for time series metrics

## Example Usage

```typescript
import { TimeSeriesBucket } from "@gram/client/models/components/timeseriesbucket.js";

let value: TimeSeriesBucket = {
  abandonedChats: 177659,
  avgSessionDurationMs: 1433.23,
  avgToolLatencyMs: 8924.08,
  bucketTimeUnixNano: "<value>",
  cacheCreationInputTokens: 421285,
  cacheReadInputTokens: 995719,
  failedChats: 950419,
  failedToolCalls: 585065,
  partialChats: 526282,
  resolvedChats: 444485,
  totalChats: 552459,
  totalCost: 5474.25,
  totalInputTokens: 93958,
  totalOutputTokens: 261304,
  totalTokens: 897482,
  totalToolCalls: 447717,
};
```

## Fields

| Field                                                           | Type                                                            | Required                                                        | Description                                                     |
| --------------------------------------------------------------- | --------------------------------------------------------------- | --------------------------------------------------------------- | --------------------------------------------------------------- |
| `abandonedChats`                                                | *number*                                                        | :heavy_check_mark:                                              | Abandoned chat sessions in this bucket                          |
| `avgSessionDurationMs`                                          | *number*                                                        | :heavy_check_mark:                                              | Average session duration in milliseconds                        |
| `avgToolLatencyMs`                                              | *number*                                                        | :heavy_check_mark:                                              | Average tool latency in milliseconds                            |
| `bucketTimeUnixNano`                                            | *string*                                                        | :heavy_check_mark:                                              | Bucket start time in Unix nanoseconds (string for JS precision) |
| `cacheCreationInputTokens`                                      | *number*                                                        | :heavy_check_mark:                                              | Sum of cache creation input tokens in this bucket               |
| `cacheReadInputTokens`                                          | *number*                                                        | :heavy_check_mark:                                              | Sum of cache read input tokens in this bucket                   |
| `failedChats`                                                   | *number*                                                        | :heavy_check_mark:                                              | Failed chat sessions in this bucket                             |
| `failedToolCalls`                                               | *number*                                                        | :heavy_check_mark:                                              | Failed tool calls in this bucket                                |
| `partialChats`                                                  | *number*                                                        | :heavy_check_mark:                                              | Partially resolved chat sessions in this bucket                 |
| `resolvedChats`                                                 | *number*                                                        | :heavy_check_mark:                                              | Resolved chat sessions in this bucket                           |
| `totalChats`                                                    | *number*                                                        | :heavy_check_mark:                                              | Total chat sessions in this bucket                              |
| `totalCost`                                                     | *number*                                                        | :heavy_check_mark:                                              | Total cost in this bucket                                       |
| `totalInputTokens`                                              | *number*                                                        | :heavy_check_mark:                                              | Sum of input tokens in this bucket                              |
| `totalOutputTokens`                                             | *number*                                                        | :heavy_check_mark:                                              | Sum of output tokens in this bucket                             |
| `totalTokens`                                                   | *number*                                                        | :heavy_check_mark:                                              | Sum of all tokens in this bucket                                |
| `totalToolCalls`                                                | *number*                                                        | :heavy_check_mark:                                              | Total tool calls in this bucket                                 |