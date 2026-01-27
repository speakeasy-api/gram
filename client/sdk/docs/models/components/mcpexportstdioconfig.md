# McpExportStdioConfig

Stdio-based MCP client configuration (Claude Desktop, Cursor)

## Example Usage

```typescript
import { McpExportStdioConfig } from "@gram/client/models/components";

let value: McpExportStdioConfig = {
  args: [
    "<value 1>",
    "<value 2>",
  ],
  command: "<value>",
};
```

## Fields

| Field                    | Type                     | Required                 | Description              |
| ------------------------ | ------------------------ | ------------------------ | ------------------------ |
| `args`                   | *string*[]               | :heavy_check_mark:       | Command arguments        |
| `command`                | *string*                 | :heavy_check_mark:       | The command to run       |
| `env`                    | Record<string, *string*> | :heavy_minus_sign:       | Environment variables    |