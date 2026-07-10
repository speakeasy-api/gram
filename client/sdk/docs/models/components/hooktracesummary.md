# HookTraceSummary

Summary information for a hook trace

## Example Usage

```typescript
import { HookTraceSummary } from "@gram/client/models/components/hooktracesummary.js";

let value: HookTraceSummary = {
  gramUrn: "<value>",
  logCount: 246899,
  startTimeUnixNano: "<value>",
  traceId: "<id>",
};
```

## Fields

| Field                                                                      | Type                                                                       | Required                                                                   | Description                                                                |
| -------------------------------------------------------------------------- | -------------------------------------------------------------------------- | -------------------------------------------------------------------------- | -------------------------------------------------------------------------- |
| `blockReason`                                                              | *string*                                                                   | :heavy_minus_sign:                                                         | Reason set when hook_status is 'blocked' (e.g. shadow-MCP guard rejection) |
| `eventSource`                                                              | *string*                                                                   | :heavy_minus_sign:                                                         | Event source (from materialized column)                                    |
| `gramUrn`                                                                  | *string*                                                                   | :heavy_check_mark:                                                         | Gram URN associated with this hook trace                                   |
| `hookSource`                                                               | *string*                                                                   | :heavy_minus_sign:                                                         | Hook source (from attributes.gram.hook.source)                             |
| `hookStatus`                                                               | [components.HookStatus](../../models/components/hookstatus.md)             | :heavy_minus_sign:                                                         | Hook execution status                                                      |
| `logCount`                                                                 | *number*                                                                   | :heavy_check_mark:                                                         | Total number of logs in this trace                                         |
| `skillName`                                                                | *string*                                                                   | :heavy_minus_sign:                                                         | Skill name (from materialized column, only for Skill tool)                 |
| `startTimeUnixNano`                                                        | *string*                                                                   | :heavy_check_mark:                                                         | Earliest log timestamp in Unix nanoseconds (string for JS int64 precision) |
| `toolName`                                                                 | *string*                                                                   | :heavy_minus_sign:                                                         | Tool name (from materialized column)                                       |
| `toolSource`                                                               | *string*                                                                   | :heavy_minus_sign:                                                         | Tool call source (from materialized column)                                |
| `traceId`                                                                  | *string*                                                                   | :heavy_check_mark:                                                         | Trace ID (32 hex characters)                                               |
| `userEmail`                                                                | *string*                                                                   | :heavy_minus_sign:                                                         | User email (from attributes.user.email)                                    |