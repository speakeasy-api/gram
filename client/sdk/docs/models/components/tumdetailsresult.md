# TumDetailsResult

Result of the billing usage details query

## Example Usage

```typescript
import { TumDetailsResult } from "@gram/client/models/components/tumdetailsresult.js";

let value: TumDetailsResult = {
  breakdowns: [
    {
      key: "<key>",
      rows: [
        {
          series: [
            556343,
            680944,
          ],
          totalTokens: 846140,
          value: "<value>",
        },
      ],
    },
  ],
  intervalSeconds: 403868,
  points: [
    {
      activeUsers: 361935,
      agentSessions: 610454,
      bucketTimeUnixNano: "<value>",
      cacheReadTokens: 608947,
      cacheWriteTokens: 619002,
      inputTokens: 887246,
      mcpToolTokens: 43332,
      outputTokens: 513029,
      riskyMessageTokens: 624272,
      skillTokens: 214782,
      toolCalls: 544936,
      toolMessageTokens: 6910,
      totalTokens: 583180,
      unattributedTokens: 123690,
    },
  ],
  totals: {
    activeUsers: 560546,
    agentSessions: 762381,
    cacheReadTokens: 358891,
    cacheWriteTokens: 472154,
    inputTokens: 560624,
    mcpToolTokens: 60258,
    outputTokens: 175249,
    riskyMessageTokens: 925575,
    skillTokens: 55189,
    toolCalls: 623308,
    toolMessageTokens: 967167,
    totalTokens: 161928,
    unattributedTokens: 395734,
  },
};
```

## Fields

| Field                                                                                                                                                                          | Type                                                                                                                                                                           | Required                                                                                                                                                                       | Description                                                                                                                                                                    |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `breakdowns`                                                                                                                                                                   | [components.TumDetailsBreakdown](../../models/components/tumdetailsbreakdown.md)[]                                                                                             | :heavy_check_mark:                                                                                                                                                             | Token usage per breakdown dimension, one entry per supported dimension                                                                                                         |
| `intervalSeconds`                                                                                                                                                              | *number*                                                                                                                                                                       | :heavy_check_mark:                                                                                                                                                             | Timeseries bucket width in seconds. Always 86400 — the details are bucketed daily.                                                                                             |
| `points`                                                                                                                                                                       | [components.TumDetailsPoint](../../models/components/tumdetailspoint.md)[]                                                                                                     | :heavy_check_mark:                                                                                                                                                             | Gap-filled daily buckets in ascending time order                                                                                                                               |
| `totals`                                                                                                                                                                       | [components.TumDetailsTotals](../../models/components/tumdetailstotals.md)                                                                                                     | :heavy_check_mark:                                                                                                                                                             | Whole-range totals for the billing usage details. Distinct counts (sessions, active users) are computed over the full range and cannot be derived by summing the daily points. |