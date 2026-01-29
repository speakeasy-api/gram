# McpExportAuthentication

Authentication requirements for the MCP server

## Example Usage

```typescript
import { McpExportAuthentication } from "@gram/client/models/components";

let value: McpExportAuthentication = {
  headers: [
    {
      displayName: "Raina.Berge81",
      name: "<value>",
    },
  ],
  required: true,
};
```

## Fields

| Field                                                                              | Type                                                                               | Required                                                                           | Description                                                                        |
| ---------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------- |
| `headers`                                                                          | [components.McpExportAuthHeader](../../models/components/mcpexportauthheader.md)[] | :heavy_check_mark:                                                                 | Required authentication headers                                                    |
| `required`                                                                         | *boolean*                                                                          | :heavy_check_mark:                                                                 | Whether authentication is required                                                 |