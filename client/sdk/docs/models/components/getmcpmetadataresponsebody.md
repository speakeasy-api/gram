# GetMcpMetadataResponseBody

## Example Usage

```typescript
import { GetMcpMetadataResponseBody } from "@gram/client/models/components/getmcpmetadataresponsebody.js";

let value: GetMcpMetadataResponseBody = {};
```

## Fields

| Field      | Type                                                             | Required           | Description                                                                                                                                   |
| ---------- | ---------------------------------------------------------------- | ------------------ | --------------------------------------------------------------------------------------------------------------------------------------------- |
| `metadata` | [components.McpMetadata](../../models/components/mcpmetadata.md) | :heavy_minus_sign: | Metadata used to configure the MCP install page. Exactly one of toolset_id or mcp_server_id identifies which backend the metadata belongs to. |
