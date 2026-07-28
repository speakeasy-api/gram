# GetToolUsageSummaryResult

Target-aware MCP and tool usage metrics

## Example Usage

```typescript
import { GetToolUsageSummaryResult } from "@gram/client/models/components/gettoolusagesummaryresult.js";

let value: GetToolUsageSummaryResult = {
  targetTimeSeries: [
    {
      bucketStartNs: "<value>",
      eventCount: 1021,
      failureCount: 6301,
      targetId: "<id>",
      targetKind: "skill",
      targetLabel: "<value>",
      targetType: "shadow_mcp_server",
    },
  ],
  targetToolBreakdown: [
    {
      eventCount: 383386,
      failureCount: 377642,
      failureRate: 6887.66,
      successCount: 612995,
      targetId: "<id>",
      targetKind: "server",
      targetLabel: "<value>",
      targetType: "tunneled_mcp_server",
      toolName: "<value>",
    },
  ],
  targets: [],
  totals: {
    eventCount: 479946,
    failureCount: 737779,
    failureRate: 1996.66,
    successCount: 855548,
    uniqueTargets: 466929,
    uniqueTools: 262933,
    uniqueUsers: 224689,
  },
  userTimeSeries: [
    {
      bucketStartNs: "<value>",
      eventCount: 420325,
      failureCount: 878923,
      userKey: "<value>",
      userKind: "user_id",
      userLabel: "<value>",
    },
  ],
  users: [
    {
      eventCount: 79028,
      failureCount: 72208,
      failureRate: 3430.87,
      successCount: 546016,
      uniqueTools: 327920,
      userKey: "<value>",
      userKind: "unknown",
      userLabel: "<value>",
    },
  ],
  usersByTarget: [
    {
      eventCount: 530887,
      failureCount: 674182,
      targetId: "<id>",
      targetKind: "server",
      targetLabel: "<value>",
      targetType: "hosted_mcp_server",
      userKey: "<value>",
      userKind: "email",
      userLabel: "<value>",
    },
  ],
};
```

## Fields

| Field                 | Type                                                                                                       | Required           | Description                                                      |
| --------------------- | ---------------------------------------------------------------------------------------------------------- | ------------------ | ---------------------------------------------------------------- |
| `targetTimeSeries`    | [components.ToolUsageTargetTimeSeriesPoint](../../models/components/toolusagetargettimeseriespoint.md)[]   | :heavy_check_mark: | Time-series usage buckets grouped by target                      |
| `targetToolBreakdown` | [components.ToolUsageTargetToolBreakdownRow](../../models/components/toolusagetargettoolbreakdownrow.md)[] | :heavy_check_mark: | Per-tool usage rows grouped by target                            |
| `targets`             | [components.ToolUsageTargetSummary](../../models/components/toolusagetargetsummary.md)[]                   | :heavy_check_mark: | Top usage targets for the selected filters and time range        |
| `totals`              | [components.ToolUsageTotals](../../models/components/toolusagetotals.md)                                   | :heavy_check_mark: | Target-aware MCP and tool usage totals                           |
| `userTimeSeries`      | [components.ToolUsageUserTimeSeriesPoint](../../models/components/toolusageusertimeseriespoint.md)[]       | :heavy_check_mark: | Time-series usage buckets grouped by user identity               |
| `users`               | [components.ToolUsageUserSummary](../../models/components/toolusageusersummary.md)[]                       | :heavy_check_mark: | Top user identities for the selected filters and time range      |
| `usersByTarget`       | [components.ToolUsageUsersByTargetRow](../../models/components/toolusageusersbytargetrow.md)[]             | :heavy_check_mark: | Cross-dimensional usage rows grouped by target and user identity |
