# HooksBreakdownRow

Cross-dimensional aggregation row: one entry per unique (user, server, hook_source, tool) combination

## Example Usage

```typescript
import { HooksBreakdownRow } from "@gram/client/models/components/hooksbreakdownrow.js";

let value: HooksBreakdownRow = {
  eventCount: 421069,
  failureCount: 198968,
  hookSource: "<value>",
  serverName: "<value>",
  toolName: "<value>",
  userEmail: "<value>",
};
```

## Fields

| Field          | Type     | Required           | Description                               |
| -------------- | -------- | ------------------ | ----------------------------------------- |
| `eventCount`   | _number_ | :heavy_check_mark: | Total events for this combination         |
| `failureCount` | _number_ | :heavy_check_mark: | Number of failures for this combination   |
| `hookSource`   | _string_ | :heavy_check_mark: | Hook source (e.g. claude-desktop, cursor) |
| `serverName`   | _string_ | :heavy_check_mark: | Server name ('local' for non-MCP tools)   |
| `toolName`     | _string_ | :heavy_check_mark: | Tool name                                 |
| `userEmail`    | _string_ | :heavy_check_mark: | User email address                        |
