# ObservabilitySummary

Aggregated summary metrics for a time period

## Example Usage

```typescript
import { ObservabilitySummary } from "@gram/client/models/components/observabilitysummary.js";

let value: ObservabilitySummary = {
  avgLatencyMs: 6770.27,
  avgResolutionTimeMs: 4069.89,
  avgSessionDurationMs: 2010.86,
  cacheCreationInputTokens: 940140,
  cacheReadInputTokens: 858200,
  failedChats: 196660,
  failedToolCalls: 532965,
  resolvedChats: 748115,
  totalChats: 538351,
  totalCost: 9162.73,
  totalInputTokens: 288667,
  totalOutputTokens: 928203,
  totalTokens: 968682,
  totalToolCalls: 233739,
};
```

## Fields

| Field                      | Type     | Required           | Description                                |
| -------------------------- | -------- | ------------------ | ------------------------------------------ |
| `avgLatencyMs`             | _number_ | :heavy_check_mark: | Average tool latency in milliseconds       |
| `avgResolutionTimeMs`      | _number_ | :heavy_check_mark: | Average time to resolution in milliseconds |
| `avgSessionDurationMs`     | _number_ | :heavy_check_mark: | Average session duration in milliseconds   |
| `cacheCreationInputTokens` | _number_ | :heavy_check_mark: | Sum of cache creation input tokens         |
| `cacheReadInputTokens`     | _number_ | :heavy_check_mark: | Sum of cache read input tokens             |
| `failedChats`              | _number_ | :heavy_check_mark: | Number of failed chat sessions             |
| `failedToolCalls`          | _number_ | :heavy_check_mark: | Number of failed tool calls                |
| `resolvedChats`            | _number_ | :heavy_check_mark: | Number of resolved chat sessions           |
| `totalChats`               | _number_ | :heavy_check_mark: | Total number of chat sessions              |
| `totalCost`                | _number_ | :heavy_check_mark: | Total cost of all requests                 |
| `totalInputTokens`         | _number_ | :heavy_check_mark: | Sum of input tokens used                   |
| `totalOutputTokens`        | _number_ | :heavy_check_mark: | Sum of output tokens used                  |
| `totalTokens`              | _number_ | :heavy_check_mark: | Sum of all tokens used                     |
| `totalToolCalls`           | _number_ | :heavy_check_mark: | Total number of tool calls                 |
