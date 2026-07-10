# McpExportTool

A tool definition in the MCP export

## Example Usage

```typescript
import { McpExportTool } from "@gram/client/models/components/mcpexporttool.js";

let value: McpExportTool = {
  description: "cinder quick tribe toward shovel midwife epic on",
  inputSchema: "<value>",
  name: "<value>",
};
```

## Fields

| Field         | Type     | Required           | Description                                 |
| ------------- | -------- | ------------------ | ------------------------------------------- |
| `description` | _string_ | :heavy_check_mark: | Description of what the tool does           |
| `inputSchema` | _any_    | :heavy_check_mark: | JSON Schema for the tool's input parameters |
| `name`        | _string_ | :heavy_check_mark: | The tool name                               |
