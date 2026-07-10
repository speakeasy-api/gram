# ToolUsageTargetToolBreakdownRow

Aggregated tool usage metrics for one target and tool

## Example Usage

```typescript
import { ToolUsageTargetToolBreakdownRow } from "@gram/client/models/components/toolusagetargettoolbreakdownrow.js";

let value: ToolUsageTargetToolBreakdownRow = {
  eventCount: 944492,
  failureCount: 889655,
  failureRate: 569.34,
  successCount: 953432,
  targetId: "<id>",
  targetKind: "server",
  targetLabel: "<value>",
  targetType: "shadow_mcp_server",
  toolName: "<value>",
};
```

## Fields

| Field                                                                                                                        | Type                                                                                                                         | Required                                                                                                                     | Description                                                                                                                  |
| ---------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------- |
| `eventCount`                                                                                                                 | *number*                                                                                                                     | :heavy_check_mark:                                                                                                           | Total number of tool usage events for the target and tool                                                                    |
| `failureCount`                                                                                                               | *number*                                                                                                                     | :heavy_check_mark:                                                                                                           | Number of failed tool usage events for the target and tool                                                                   |
| `failureRate`                                                                                                                | *number*                                                                                                                     | :heavy_check_mark:                                                                                                           | Fraction of completed tool usage events for the target and tool that failed                                                  |
| `successCount`                                                                                                               | *number*                                                                                                                     | :heavy_check_mark:                                                                                                           | Number of successful tool usage events for the target and tool                                                               |
| `targetId`                                                                                                                   | *string*                                                                                                                     | :heavy_check_mark:                                                                                                           | Stable target identifier used by filters and chart grouping                                                                  |
| `targetKind`                                                                                                                 | [components.ToolUsageTargetToolBreakdownRowTargetKind](../../models/components/toolusagetargettoolbreakdownrowtargetkind.md) | :heavy_check_mark:                                                                                                           | Tool usage aggregation target kind                                                                                           |
| `targetLabel`                                                                                                                | *string*                                                                                                                     | :heavy_check_mark:                                                                                                           | User-facing label for the target                                                                                             |
| `targetType`                                                                                                                 | [components.ToolUsageTargetToolBreakdownRowTargetType](../../models/components/toolusagetargettoolbreakdownrowtargettype.md) | :heavy_check_mark:                                                                                                           | Tool usage target type                                                                                                       |
| `toolName`                                                                                                                   | *string*                                                                                                                     | :heavy_check_mark:                                                                                                           | Observed tool name                                                                                                           |