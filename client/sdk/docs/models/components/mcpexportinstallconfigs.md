# McpExportInstallConfigs

Installation configurations for different MCP clients

## Example Usage

```typescript
import { McpExportInstallConfigs } from "@gram/client/models/components";

let value: McpExportInstallConfigs = {
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
};
```

## Fields

| Field                                                                              | Type                                                                               | Required                                                                           | Description                                                                        |
| ---------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------- |
| `claudeCode`                                                                       | *string*                                                                           | :heavy_check_mark:                                                                 | CLI command for Claude Code                                                        |
| `claudeDesktop`                                                                    | [components.McpExportStdioConfig](../../models/components/mcpexportstdioconfig.md) | :heavy_check_mark:                                                                 | Stdio-based MCP client configuration (Claude Desktop, Cursor)                      |
| `cursor`                                                                           | [components.McpExportStdioConfig](../../models/components/mcpexportstdioconfig.md) | :heavy_check_mark:                                                                 | Stdio-based MCP client configuration (Claude Desktop, Cursor)                      |
| `vscode`                                                                           | [components.McpExportHTTPConfig](../../models/components/mcpexporthttpconfig.md)   | :heavy_check_mark:                                                                 | HTTP-based MCP client configuration (VS Code)                                      |