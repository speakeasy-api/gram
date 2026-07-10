# ToolUsageTraceSummary

A single target-aware tool usage trace row

## Example Usage

```typescript
import { ToolUsageTraceSummary } from "@gram/client/models/components/toolusagetracesummary.js";

let value: ToolUsageTraceSummary = {
  eventSource: "<value>",
  gramUrn: "<value>",
  id: "<id>",
  logCount: 182750,
  logGroup: {
    kind: "log_id",
    value: "<value>",
  },
  startTimeUnixNano: "<value>",
  targetId: "<id>",
  targetKind: "local_tools",
  targetLabel: "<value>",
  targetType: "shadow_mcp_server",
  toolName: "<value>",
  userKey: "<value>",
  userKind: "external_user_id",
  userLabel: "<value>",
};
```

## Fields

| Field               | Type                                                                                                     | Required           | Description                                                                          |
| ------------------- | -------------------------------------------------------------------------------------------------------- | ------------------ | ------------------------------------------------------------------------------------ |
| `accountType`       | _string_                                                                                                 | :heavy_minus_sign: | AI account classification ('team' or 'personal'); empty/absent when unclassified     |
| `blockReason`       | _string_                                                                                                 | :heavy_minus_sign: | Hook block reason when hook_status is blocked                                        |
| `eventSource`       | _string_                                                                                                 | :heavy_check_mark: | Telemetry event source                                                               |
| `gramUrn`           | _string_                                                                                                 | :heavy_check_mark: | Gram URN associated with the trace                                                   |
| `hookSource`        | _string_                                                                                                 | :heavy_minus_sign: | Hook plugin source when the row came from hook telemetry                             |
| `hookStatus`        | [components.ToolUsageTraceSummaryHookStatus](../../models/components/toolusagetracesummaryhookstatus.md) | :heavy_minus_sign: | Hook execution status when the row came from hook telemetry                          |
| `httpStatusCode`    | _number_                                                                                                 | :heavy_minus_sign: | HTTP status code when available                                                      |
| `id`                | _string_                                                                                                 | :heavy_check_mark: | Stable row identity for React keys and expansion state                               |
| `logCount`          | _number_                                                                                                 | :heavy_check_mark: | Number of logs in the trace                                                          |
| `logGroup`          | [components.ToolUsageTraceLogGroup](../../models/components/toolusagetraceloggroup.md)                   | :heavy_check_mark: | Descriptor used by the dashboard to fetch child logs for a trace row                 |
| `startTimeUnixNano` | _string_                                                                                                 | :heavy_check_mark: | Earliest log timestamp in Unix nanoseconds as a string for JavaScript integer safety |
| `targetId`          | _string_                                                                                                 | :heavy_check_mark: | Stable target identifier used by filters                                             |
| `targetKind`        | [components.ToolUsageTraceSummaryTargetKind](../../models/components/toolusagetracesummarytargetkind.md) | :heavy_check_mark: | Tool usage aggregation target kind                                                   |
| `targetLabel`       | _string_                                                                                                 | :heavy_check_mark: | User-facing target label                                                             |
| `targetType`        | [components.ToolUsageTraceSummaryTargetType](../../models/components/toolusagetracesummarytargettype.md) | :heavy_check_mark: | Tool usage target type                                                               |
| `toolName`          | _string_                                                                                                 | :heavy_check_mark: | Tool name shown in the row                                                           |
| `traceId`           | _string_                                                                                                 | :heavy_minus_sign: | Real OTel trace ID when the grouped logs have one                                    |
| `userKey`           | _string_                                                                                                 | :heavy_check_mark: | Stable user identity value                                                           |
| `userKind`          | [components.ToolUsageTraceSummaryUserKind](../../models/components/toolusagetracesummaryuserkind.md)     | :heavy_check_mark: | Tool usage user identity kind                                                        |
| `userLabel`         | _string_                                                                                                 | :heavy_check_mark: | User-facing user identity label                                                      |
