# ListCatalogResponseBody

## Example Usage

```typescript
import { ListCatalogResponseBody } from "@gram/client/models/components/listcatalogresponsebody.js";

let value: ListCatalogResponseBody = {
  servers: [
    {
      description: "finally word tall",
      isReadOnly: false,
      registrySpecifier: "io.modelcontextprotocol.anonymous/exa",
      supportsDcr: false,
      toolCount: 39512,
      version: "1.0.0",
    },
  ],
};
```

## Fields

| Field        | Type                                                                                     | Required           | Description                         |
| ------------ | ---------------------------------------------------------------------------------------- | ------------------ | ----------------------------------- |
| `nextCursor` | _string_                                                                                 | :heavy_minus_sign: | Pagination cursor for the next page |
| `servers`    | [components.ExternalMCPServerEntry](../../models/components/externalmcpserverentry.md)[] | :heavy_check_mark: | List of available MCP servers       |
