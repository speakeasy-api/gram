# McpExportHTTPConfig

HTTP-based MCP client configuration (VS Code)

## Example Usage

```typescript
import { McpExportHTTPConfig } from "@gram/client/models/components";

let value: McpExportHTTPConfig = {
  type: "<value>",
  url: "https://well-documented-affect.com",
};
```

## Fields

| Field                                               | Type                                                | Required                                            | Description                                         |
| --------------------------------------------------- | --------------------------------------------------- | --------------------------------------------------- | --------------------------------------------------- |
| `headers`                                           | Record<string, *string*>                            | :heavy_minus_sign:                                  | HTTP headers with environment variable placeholders |
| `type`                                              | *string*                                            | :heavy_check_mark:                                  | Transport type (always 'http')                      |
| `url`                                               | *string*                                            | :heavy_check_mark:                                  | The MCP server URL                                  |