# AddPluginServerForm

## Example Usage

```typescript
import { AddPluginServerForm } from "@gram/client/models/components/addpluginserverform.js";

let value: AddPluginServerForm = {
  pluginId: "6ad945d2-ec55-43e0-8f58-18fc895642a1",
};
```

## Fields

| Field         | Type                                                   | Required           | Description                                                                                            |
| ------------- | ------------------------------------------------------ | ------------------ | ------------------------------------------------------------------------------------------------------ |
| `displayName` | _string_                                               | :heavy_minus_sign: | Display name for the server. Defaults to the backing toolset or mcp_server name when omitted.          |
| `mcpServerId` | _string_                                               | :heavy_minus_sign: | Gram MCP server ID for a Remote MCP-backed server. Provide exactly one of toolset_id or mcp_server_id. |
| `pluginId`    | _string_                                               | :heavy_check_mark: | N/A                                                                                                    |
| `policy`      | [components.Policy](../../models/components/policy.md) | :heavy_minus_sign: | N/A                                                                                                    |
| `sortOrder`   | _number_                                               | :heavy_minus_sign: | N/A                                                                                                    |
| `toolsetId`   | _string_                                               | :heavy_minus_sign: | Gram toolset ID for a toolset-backed MCP server. Provide exactly one of toolset_id or mcp_server_id.   |
