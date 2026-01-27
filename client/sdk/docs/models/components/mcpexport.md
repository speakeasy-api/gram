# McpExport

Complete MCP server export for documentation and integration

## Example Usage

```typescript
import { McpExport } from "@gram/client/models/components";

let value: McpExport = {
  authentication: {
    headers: [],
    required: false,
  },
  installConfigs: {
    claudeCode: "<value>",
    claudeDesktop: {
      args: [],
      command: "<value>",
    },
    cursor: {
      args: [
        "<value 1>",
        "<value 2>",
      ],
      command: "<value>",
    },
    vscode: {
      type: "<value>",
      url: "https://tired-contrail.name/",
    },
  },
  name: "<value>",
  serverUrl: "https://whimsical-meatloaf.org/",
  slug: "<value>",
  tools: [],
};
```

## Fields

| Field                                                                                    | Type                                                                                     | Required                                                                                 | Description                                                                              |
| ---------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------- |
| `authentication`                                                                         | [components.McpExportAuthentication](../../models/components/mcpexportauthentication.md) | :heavy_check_mark:                                                                       | Authentication requirements for the MCP server                                           |
| `description`                                                                            | *string*                                                                                 | :heavy_minus_sign:                                                                       | Description of the MCP server                                                            |
| `documentationUrl`                                                                       | *string*                                                                                 | :heavy_minus_sign:                                                                       | Link to external documentation                                                           |
| `installConfigs`                                                                         | [components.McpExportInstallConfigs](../../models/components/mcpexportinstallconfigs.md) | :heavy_check_mark:                                                                       | Installation configurations for different MCP clients                                    |
| `instructions`                                                                           | *string*                                                                                 | :heavy_minus_sign:                                                                       | Server instructions for users                                                            |
| `logoUrl`                                                                                | *string*                                                                                 | :heavy_minus_sign:                                                                       | URL to the server logo                                                                   |
| `name`                                                                                   | *string*                                                                                 | :heavy_check_mark:                                                                       | The MCP server name                                                                      |
| `serverUrl`                                                                              | *string*                                                                                 | :heavy_check_mark:                                                                       | The MCP server URL                                                                       |
| `slug`                                                                                   | *string*                                                                                 | :heavy_check_mark:                                                                       | The MCP server slug                                                                      |
| `tools`                                                                                  | [components.McpExportTool](../../models/components/mcpexporttool.md)[]                   | :heavy_check_mark:                                                                       | Available tools on this MCP server                                                       |