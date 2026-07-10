# TopServer

Top MCP server by tool call count

## Example Usage

```typescript
import { TopServer } from "@gram/client/models/components/topserver.js";

let value: TopServer = {
  serverName: "<value>",
  toolCallCount: 729804,
};
```

## Fields

| Field                      | Type                       | Required                   | Description                |
| -------------------------- | -------------------------- | -------------------------- | -------------------------- |
| `serverName`               | *string*                   | :heavy_check_mark:         | MCP server name            |
| `toolCallCount`            | *number*                   | :heavy_check_mark:         | Total number of tool calls |