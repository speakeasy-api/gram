# GetMcpMetadataRequest

## Example Usage

```typescript
import { GetMcpMetadataRequest } from "@gram/client/models/operations/getmcpmetadata.js";

let value: GetMcpMetadataRequest = {};
```

## Fields

| Field                                                                                                      | Type                                                                                                       | Required                                                                                                   | Description                                                                                                |
| ---------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------- |
| `toolsetSlug`                                                                                              | *string*                                                                                                   | :heavy_minus_sign:                                                                                         | The slug of the toolset associated with this install page metadata. Mutually exclusive with mcp_server_id. |
| `mcpServerId`                                                                                              | *string*                                                                                                   | :heavy_minus_sign:                                                                                         | The ID of the MCP server associated with this install page metadata. Mutually exclusive with toolset_slug. |
| `gramKey`                                                                                                  | *string*                                                                                                   | :heavy_minus_sign:                                                                                         | API Key header                                                                                             |
| `gramSession`                                                                                              | *string*                                                                                                   | :heavy_minus_sign:                                                                                         | Session header                                                                                             |
| `gramProject`                                                                                              | *string*                                                                                                   | :heavy_minus_sign:                                                                                         | project header                                                                                             |