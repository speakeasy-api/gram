# GetUserMetricsSummaryResult

Result of user metrics summary query

## Example Usage

```typescript
import { GetUserMetricsSummaryResult } from "@gram/client/models/components/getusermetricssummaryresult.js";

let value: GetUserMetricsSummaryResult = {
  metrics: {
    avgChatDurationMs: 830.94,
    avgChatResolutionScore: 6964.42,
    avgTokensPerRequest: 6738.55,
    avgToolDurationMs: 5253.17,
    cacheCreationInputTokens: 119582,
    cacheReadInputTokens: 457428,
    chatResolutionAbandoned: 574466,
    chatResolutionFailure: 767239,
    chatResolutionPartial: 223673,
    chatResolutionSuccess: 550211,
    distinctModels: 578723,
    distinctProviders: 883179,
    finishReasonStop: 72102,
    finishReasonToolCalls: 634535,
    firstSeenUnixNano: "<value>",
    lastSeenUnixNano: "<value>",
    models: [
      {
        count: 121591,
        name: "<value>",
      },
    ],
    toolCallFailure: 755192,
    toolCallSuccess: 84624,
    tools: [
      {
        count: 516105,
        failureCount: 521684,
        successCount: 267906,
        urn: "<value>",
      },
    ],
    totalChatRequests: 763811,
    totalChats: 470953,
    totalCost: 5442.49,
    totalInputTokens: 386673,
    totalOutputTokens: 455569,
    totalTokens: 781343,
    totalToolCalls: 860429,
  },
};
```

## Fields

| Field     | Type                                                                   | Required           | Description        |
| --------- | ---------------------------------------------------------------------- | ------------------ | ------------------ |
| `metrics` | [components.ProjectSummary](../../models/components/projectsummary.md) | :heavy_check_mark: | Aggregated metrics |
