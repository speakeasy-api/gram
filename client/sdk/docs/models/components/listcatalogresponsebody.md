# ListCatalogResponseBody

## Example Usage

```typescript
import { ListCatalogResponseBody } from "@gram/client/models/components";

let value: ListCatalogResponseBody = {
  servers: [
    {
      description: "finally word tall",
      name: "io.modelcontextprotocol.anonymous/exa",
      registryId: "bb0ff565-978d-483e-b197-ab1951c837b1",
      version: "1.0.0",
    },
  ],
};
```

## Fields

| Field                                                                          | Type                                                                           | Required                                                                       | Description                                                                    |
| ------------------------------------------------------------------------------ | ------------------------------------------------------------------------------ | ------------------------------------------------------------------------------ | ------------------------------------------------------------------------------ |
| `nextCursor`                                                                   | *string*                                                                       | :heavy_minus_sign:                                                             | Pagination cursor for the next page                                            |
| `servers`                                                                      | [components.ExternalMCPServer](../../models/components/externalmcpserver.md)[] | :heavy_check_mark:                                                             | List of available MCP servers                                                  |