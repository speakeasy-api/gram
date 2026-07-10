# PluginServer

## Example Usage

```typescript
import { PluginServer } from "@gram/client/models/components/pluginserver.js";

let value: PluginServer = {
  createdAt: new Date("2025-02-05T12:32:59.237Z"),
  displayName: "Zita_Renner16",
  id: "082fa0ff-a332-46f4-8787-69ab4f273ebd",
  policy: "required",
  sortOrder: 709284,
};
```

## Fields

| Field                                                                                                             | Type                                                                                                              | Required                                                                                                          | Description                                                                                                       |
| ----------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------- |
| `createdAt`                                                                                                       | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date)                     | :heavy_check_mark:                                                                                                | N/A                                                                                                               |
| `displayName`                                                                                                     | *string*                                                                                                          | :heavy_check_mark:                                                                                                | Display name shown in generated plugin config.                                                                    |
| `id`                                                                                                              | *string*                                                                                                          | :heavy_check_mark:                                                                                                | Unique plugin server identifier.                                                                                  |
| `mcpServerId`                                                                                                     | *string*                                                                                                          | :heavy_minus_sign:                                                                                                | Gram MCP server ID. Set when this server is Remote MCP-backed (exactly one of toolset_id / mcp_server_id is set). |
| `policy`                                                                                                          | [components.PluginServerPolicy](../../models/components/pluginserverpolicy.md)                                    | :heavy_check_mark:                                                                                                | Whether this server is required or optional.                                                                      |
| `sortOrder`                                                                                                       | *number*                                                                                                          | :heavy_check_mark:                                                                                                | Ordering within the plugin.                                                                                       |
| `toolsetId`                                                                                                       | *string*                                                                                                          | :heavy_minus_sign:                                                                                                | Gram toolset ID. Set when this server is toolset-backed (exactly one of toolset_id / mcp_server_id is set).       |