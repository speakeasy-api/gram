# ListMcpEndpointsResult

Result type for listing MCP endpoints

## Example Usage

```typescript
import { ListMcpEndpointsResult } from "@gram/client/models/components/listmcpendpointsresult.js";

let value: ListMcpEndpointsResult = {
  mcpEndpoints: [
    {
      createdAt: new Date("2024-06-06T15:43:20.829Z"),
      id: "354a8406-3bfc-458c-942f-53d352905a78",
      mcpServerId: "638937d7-baaf-45dc-ba70-fdf526a5a6ae",
      projectId: "7cd26f66-291f-48f7-8113-0aa0098c5260",
      slug: "<value>",
      updatedAt: new Date("2025-12-16T16:22:04.421Z"),
    },
  ],
};
```

## Fields

| Field          | Type                                                               | Required           | Description |
| -------------- | ------------------------------------------------------------------ | ------------------ | ----------- |
| `mcpEndpoints` | [components.McpEndpoint](../../models/components/mcpendpoint.md)[] | :heavy_check_mark: | N/A         |
