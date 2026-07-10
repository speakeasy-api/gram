# GetObservabilityOverviewResult

Result of observability overview query

## Example Usage

```typescript
import { GetObservabilityOverviewResult } from "@gram/client/models/components/getobservabilityoverviewresult.js";

let value: GetObservabilityOverviewResult = {
  comparison: {
    avgLatencyMs: 7661.1,
    avgResolutionTimeMs: 4695.8,
    avgSessionDurationMs: 7311.88,
    cacheCreationInputTokens: 450191,
    cacheReadInputTokens: 220317,
    failedChats: 438678,
    failedToolCalls: 927086,
    resolvedChats: 554966,
    totalChats: 96952,
    totalCost: 2822.02,
    totalInputTokens: 419955,
    totalOutputTokens: 71641,
    totalTokens: 131455,
    totalToolCalls: 43753,
  },
  intervalSeconds: 680906,
  summary: {
    avgLatencyMs: 5896.12,
    avgResolutionTimeMs: 7983.73,
    avgSessionDurationMs: 4517.3,
    cacheCreationInputTokens: 694643,
    cacheReadInputTokens: 590455,
    failedChats: 918906,
    failedToolCalls: 208225,
    resolvedChats: 658630,
    totalChats: 649443,
    totalCost: 2099.66,
    totalInputTokens: 885881,
    totalOutputTokens: 263970,
    totalTokens: 125415,
    totalToolCalls: 749939,
  },
  timeSeries: [],
  topToolsByCount: [
    {
      avgLatencyMs: 1421.66,
      callCount: 265456,
      failureCount: 329040,
      failureRate: 1111.25,
      gramUrn: "<value>",
      successCount: 28239,
    },
  ],
  topToolsByFailureRate: [
    {
      avgLatencyMs: 9233.77,
      callCount: 148040,
      failureCount: 137660,
      failureRate: 1250.33,
      gramUrn: "<value>",
      successCount: 803966,
    },
  ],
};
```

## Fields

| Field                                                                              | Type                                                                               | Required                                                                           | Description                                                                        |
| ---------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------- |
| `comparison`                                                                       | [components.ObservabilitySummary](../../models/components/observabilitysummary.md) | :heavy_check_mark:                                                                 | Aggregated summary metrics for a time period                                       |
| `intervalSeconds`                                                                  | *number*                                                                           | :heavy_check_mark:                                                                 | The time bucket interval in seconds used for the time series data                  |
| `summary`                                                                          | [components.ObservabilitySummary](../../models/components/observabilitysummary.md) | :heavy_check_mark:                                                                 | Aggregated summary metrics for a time period                                       |
| `timeSeries`                                                                       | [components.TimeSeriesBucket](../../models/components/timeseriesbucket.md)[]       | :heavy_check_mark:                                                                 | Time series data points                                                            |
| `topToolsByCount`                                                                  | [components.ToolMetric](../../models/components/toolmetric.md)[]                   | :heavy_check_mark:                                                                 | Top tools by call count                                                            |
| `topToolsByFailureRate`                                                            | [components.ToolMetric](../../models/components/toolmetric.md)[]                   | :heavy_check_mark:                                                                 | Top tools by failure rate                                                          |