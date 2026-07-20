# McpExport

Complete MCP server export for documentation and integration

## Example Usage

```typescript
import { McpExport } from "@gram/client/models/components/mcpexport.js";

let value: McpExport = {
  authentication: {
    headers: [],
    required: false,
  },
  name: "<value>",
  serverUrl: "https://near-bonnet.org/",
  slug: "<value>",
  tools: [],
};
```

## Fields

| Field              | Type                                                                                     | Required           | Description                                    |
| ------------------ | ---------------------------------------------------------------------------------------- | ------------------ | ---------------------------------------------- |
| `authentication`   | [components.McpExportAuthentication](../../models/components/mcpexportauthentication.md) | :heavy_check_mark: | Authentication requirements for the MCP server |
| `description`      | _string_                                                                                 | :heavy_minus_sign: | Description of the MCP server                  |
| `documentationUrl` | _string_                                                                                 | :heavy_minus_sign: | Link to external documentation                 |
| `instructions`     | _string_                                                                                 | :heavy_minus_sign: | Server instructions for users                  |
| `logoUrl`          | _string_                                                                                 | :heavy_minus_sign: | URL to the server logo                         |
| `name`             | _string_                                                                                 | :heavy_check_mark: | The MCP server name                            |
| `serverUrl`        | _string_                                                                                 | :heavy_check_mark: | The MCP server URL                             |
| `slug`             | _string_                                                                                 | :heavy_check_mark: | The MCP server slug                            |
| `tools`            | [components.McpExportTool](../../models/components/mcpexporttool.md)[]                   | :heavy_check_mark: | Available tools on this MCP server             |
