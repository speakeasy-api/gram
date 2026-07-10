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

| Field           | Type     | Required           | Description                |
| --------------- | -------- | ------------------ | -------------------------- |
| `serverName`    | _string_ | :heavy_check_mark: | MCP server name            |
| `toolCallCount` | _number_ | :heavy_check_mark: | Total number of tool calls |
