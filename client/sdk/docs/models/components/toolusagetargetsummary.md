# ToolUsageTargetSummary

Aggregated tool usage metrics for one target

## Example Usage

```typescript
import { ToolUsageTargetSummary } from "@gram/client/models/components/toolusagetargetsummary.js";

let value: ToolUsageTargetSummary = {
  eventCount: 236394,
  failureCount: 841526,
  failureRate: 8556.16,
  successCount: 973897,
  targetId: "<id>",
  targetKind: "local_tools",
  targetLabel: "<value>",
  targetType: "shadow_mcp_server",
  uniqueTools: 471642,
};
```

## Fields

| Field                                                                                                      | Type                                                                                                       | Required                                                                                                   | Description                                                                                                |
| ---------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------- |
| `eventCount`                                                                                               | *number*                                                                                                   | :heavy_check_mark:                                                                                         | Total number of tool usage events for the target                                                           |
| `failureCount`                                                                                             | *number*                                                                                                   | :heavy_check_mark:                                                                                         | Number of failed tool usage events for the target                                                          |
| `failureRate`                                                                                              | *number*                                                                                                   | :heavy_check_mark:                                                                                         | Fraction of completed tool usage events for the target that failed                                         |
| `successCount`                                                                                             | *number*                                                                                                   | :heavy_check_mark:                                                                                         | Number of successful tool usage events for the target                                                      |
| `targetId`                                                                                                 | *string*                                                                                                   | :heavy_check_mark:                                                                                         | Stable target identifier used by filters and chart grouping                                                |
| `targetKind`                                                                                               | [components.ToolUsageTargetSummaryTargetKind](../../models/components/toolusagetargetsummarytargetkind.md) | :heavy_check_mark:                                                                                         | Tool usage aggregation target kind                                                                         |
| `targetLabel`                                                                                              | *string*                                                                                                   | :heavy_check_mark:                                                                                         | User-facing label for the target                                                                           |
| `targetType`                                                                                               | [components.ToolUsageTargetSummaryTargetType](../../models/components/toolusagetargetsummarytargettype.md) | :heavy_check_mark:                                                                                         | Tool usage target type                                                                                     |
| `uniqueTools`                                                                                              | *number*                                                                                                   | :heavy_check_mark:                                                                                         | Number of distinct tools observed for the target                                                           |