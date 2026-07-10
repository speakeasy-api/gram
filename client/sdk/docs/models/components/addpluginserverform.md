# AddPluginServerForm

## Example Usage

```typescript
import { AddPluginServerForm } from "@gram/client/models/components/addpluginserverform.js";

let value: AddPluginServerForm = {
  pluginId: "6ad945d2-ec55-43e0-8f58-18fc895642a1",
};
```

## Fields

| Field                                                                                                  | Type                                                                                                   | Required                                                                                               | Description                                                                                            |
| ------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------ |
| `displayName`                                                                                          | *string*                                                                                               | :heavy_minus_sign:                                                                                     | Display name for the server. Defaults to the backing toolset or mcp_server name when omitted.          |
| `mcpServerId`                                                                                          | *string*                                                                                               | :heavy_minus_sign:                                                                                     | Gram MCP server ID for a Remote MCP-backed server. Provide exactly one of toolset_id or mcp_server_id. |
| `pluginId`                                                                                             | *string*                                                                                               | :heavy_check_mark:                                                                                     | N/A                                                                                                    |
| `policy`                                                                                               | [components.Policy](../../models/components/policy.md)                                                 | :heavy_minus_sign:                                                                                     | N/A                                                                                                    |
| `sortOrder`                                                                                            | *number*                                                                                               | :heavy_minus_sign:                                                                                     | N/A                                                                                                    |
| `toolsetId`                                                                                            | *string*                                                                                               | :heavy_minus_sign:                                                                                     | Gram toolset ID for a toolset-backed MCP server. Provide exactly one of toolset_id or mcp_server_id.   |