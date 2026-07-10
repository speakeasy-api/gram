# GetMcpMetadataRequest

## Example Usage

```typescript
import { GetMcpMetadataRequest } from "@gram/client/models/operations/getmcpmetadata.js";

let value: GetMcpMetadataRequest = {};
```

## Fields

| Field         | Type     | Required           | Description                                                                                                |
| ------------- | -------- | ------------------ | ---------------------------------------------------------------------------------------------------------- |
| `toolsetSlug` | _string_ | :heavy_minus_sign: | The slug of the toolset associated with this install page metadata. Mutually exclusive with mcp_server_id. |
| `mcpServerId` | _string_ | :heavy_minus_sign: | The ID of the MCP server associated with this install page metadata. Mutually exclusive with toolset_slug. |
| `gramKey`     | _string_ | :heavy_minus_sign: | API Key header                                                                                             |
| `gramSession` | _string_ | :heavy_minus_sign: | Session header                                                                                             |
| `gramProject` | _string_ | :heavy_minus_sign: | project header                                                                                             |
