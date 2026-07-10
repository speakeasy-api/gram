# ToolUsageTraceLogGroup

Descriptor used by the dashboard to fetch child logs for a trace row

## Example Usage

```typescript
import { ToolUsageTraceLogGroup } from "@gram/client/models/components/toolusagetraceloggroup.js";

let value: ToolUsageTraceLogGroup = {
  kind: "trigger_event_id",
  value: "<value>",
};
```

## Fields

| Field                                                                                          | Type                                                                                           | Required                                                                                       | Description                                                                                    |
| ---------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------- |
| `kind`                                                                                         | [components.ToolUsageTraceLogGroupKind](../../models/components/toolusagetraceloggroupkind.md) | :heavy_check_mark:                                                                             | Child-log lookup strategy for a tool usage trace row                                           |
| `value`                                                                                        | *string*                                                                                       | :heavy_check_mark:                                                                             | Lookup value                                                                                   |