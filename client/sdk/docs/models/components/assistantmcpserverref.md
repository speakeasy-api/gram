# AssistantMCPServerRef

## Example Usage

```typescript
import { AssistantMCPServerRef } from "@gram/client/models/components/assistantmcpserverref.js";

let value: AssistantMCPServerRef = {
  mcpServerSlug: "<value>",
};
```

## Fields

| Field             | Type     | Required           | Description                                                                                                                                                          |
| ----------------- | -------- | ------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `endpointSlug`    | _string_ | :heavy_minus_sign: | The slug of the server's Gram-hosted MCP endpoint (/mcp/{endpoint_slug}). Populated on reads; ignored on writes. Absent when the server has no Gram-hosted endpoint. |
| `environmentSlug` | _string_ | :heavy_minus_sign: | Optional environment slug used when connecting to the MCP server.                                                                                                    |
| `mcpServerSlug`   | _string_ | :heavy_check_mark: | The MCP server slug exposed to the assistant. Covers remote- and tunnelled-backed MCP servers, which have no toolset to attach.                                      |
