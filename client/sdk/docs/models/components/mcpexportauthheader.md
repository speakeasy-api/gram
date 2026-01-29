# McpExportAuthHeader

An authentication header required by the MCP server

## Example Usage

```typescript
import { McpExportAuthHeader } from "@gram/client/models/components";

let value: McpExportAuthHeader = {
  displayName: "Alaina42",
  name: "<value>",
};
```

## Fields

| Field                                      | Type                                       | Required                                   | Description                                |
| ------------------------------------------ | ------------------------------------------ | ------------------------------------------ | ------------------------------------------ |
| `displayName`                              | *string*                                   | :heavy_check_mark:                         | User-friendly display name (e.g., API Key) |
| `name`                                     | *string*                                   | :heavy_check_mark:                         | The HTTP header name (e.g., Authorization) |