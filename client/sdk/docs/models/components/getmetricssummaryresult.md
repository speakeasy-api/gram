# GetMetricsSummaryResult

Result of metrics summary query

## Example Usage

```typescript
import { GetMetricsSummaryResult } from "@gram/client/models/components";

let value: GetMetricsSummaryResult = {
  enabled: true,
  metrics: {
    avgChatDurationMs: 6964.42,
    avgTokensPerRequest: 6738.55,
    avgToolDurationMs: 5253.17,
    distinctModels: 119582,
    distinctProviders: 457428,
    finishReasonStop: 574466,
    finishReasonToolCalls: 767239,
    models: [],
    toolCallFailure: 550211,
    toolCallSuccess: 578723,
    tools: [
      {
        count: 72102,
        failureCount: 634535,
        successCount: 680287,
        urn: "<value>",
      },
    ],
    totalChatRequests: 121591,
    totalChats: 755192,
    totalInputTokens: 84624,
    totalOutputTokens: 962622,
    totalTokens: 516105,
    totalToolCalls: 521684,
  },
};
```

## Fields

| Field                                                    | Type                                                     | Required                                                 | Description                                              |
| -------------------------------------------------------- | -------------------------------------------------------- | -------------------------------------------------------- | -------------------------------------------------------- |
| `enabled`                                                | *boolean*                                                | :heavy_check_mark:                                       | Whether telemetry is enabled for the organization        |
| `metrics`                                                | [components.Metrics](../../models/components/metrics.md) | :heavy_check_mark:                                       | Aggregated metrics                                       |