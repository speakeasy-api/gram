# ToolCallSummary

Summary information for a tool call

## Example Usage

```typescript
import { ToolCallSummary } from "@gram/client/models/components/toolcallsummary.js";

let value: ToolCallSummary = {
  gramUrn: "<value>",
  logCount: 994472,
  startTimeUnixNano: "<value>",
  traceId: "<id>",
};
```

## Fields

| Field               | Type     | Required           | Description                                                                |
| ------------------- | -------- | ------------------ | -------------------------------------------------------------------------- |
| `eventSource`       | _string_ | :heavy_minus_sign: | Event source (from attributes.gram.event.source)                           |
| `gramUrn`           | _string_ | :heavy_check_mark: | Gram URN associated with this tool call                                    |
| `httpStatusCode`    | _number_ | :heavy_minus_sign: | HTTP status code (if applicable)                                           |
| `logCount`          | _number_ | :heavy_check_mark: | Total number of logs in this tool call                                     |
| `startTimeUnixNano` | _string_ | :heavy_check_mark: | Earliest log timestamp in Unix nanoseconds (string for JS int64 precision) |
| `toolName`          | _string_ | :heavy_minus_sign: | Tool name (from attributes.gram.tool.name)                                 |
| `toolSource`        | _string_ | :heavy_minus_sign: | Tool call source (from attributes.gram.tool_call.source)                   |
| `traceId`           | _string_ | :heavy_check_mark: | Trace ID (32 hex characters)                                               |
