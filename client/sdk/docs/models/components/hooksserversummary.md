# HooksServerSummary

Aggregated hooks metrics for a single server

## Example Usage

```typescript
import { HooksServerSummary } from "@gram/client/models/components/hooksserversummary.js";

let value: HooksServerSummary = {
  eventCount: 99445,
  failureCount: 967935,
  failureRate: 1897.94,
  serverName: "<value>",
  successCount: 949214,
  uniqueTools: 194993,
};
```

## Fields

| Field          | Type     | Required           | Description                                                          |
| -------------- | -------- | ------------------ | -------------------------------------------------------------------- |
| `eventCount`   | _number_ | :heavy_check_mark: | Total number of hook events for this server                          |
| `failureCount` | _number_ | :heavy_check_mark: | Number of failed tool completions (PostToolUseFailure events)        |
| `failureRate`  | _number_ | :heavy_check_mark: | Failure rate as a decimal (0.0 to 1.0)                               |
| `serverName`   | _string_ | :heavy_check_mark: | Server name (extracted from tool name, or 'local' for non-MCP tools) |
| `successCount` | _number_ | :heavy_check_mark: | Number of successful tool completions (PostToolUse events)           |
| `uniqueTools`  | _number_ | :heavy_check_mark: | Number of unique tools used for this server                          |
