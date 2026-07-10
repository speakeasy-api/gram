# ExternalMCPRemoteHeader

A header requirement for a remote MCP server

## Example Usage

```typescript
import { ExternalMCPRemoteHeader } from "@gram/client/models/components/externalmcpremoteheader.js";

let value: ExternalMCPRemoteHeader = {
  name: "<value>",
};
```

## Fields

| Field                                                 | Type                                                  | Required                                              | Description                                           |
| ----------------------------------------------------- | ----------------------------------------------------- | ----------------------------------------------------- | ----------------------------------------------------- |
| `description`                                         | *string*                                              | :heavy_minus_sign:                                    | Description of the header                             |
| `isRequired`                                          | *boolean*                                             | :heavy_minus_sign:                                    | Whether this header is required                       |
| `isSecret`                                            | *boolean*                                             | :heavy_minus_sign:                                    | Whether this header value should be treated as secret |
| `name`                                                | *string*                                              | :heavy_check_mark:                                    | Header name                                           |
| `placeholder`                                         | *string*                                              | :heavy_minus_sign:                                    | Placeholder value to show when collecting this header |