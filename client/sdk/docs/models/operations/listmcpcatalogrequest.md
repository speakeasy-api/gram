# ListMcpCatalogRequest

## Example Usage

```typescript
import { ListMcpCatalogRequest } from "@gram/client/models/operations";

let value: ListMcpCatalogRequest = {};
```

## Fields

| Field                                  | Type                                   | Required                               | Description                            |
| -------------------------------------- | -------------------------------------- | -------------------------------------- | -------------------------------------- |
| `registryId`                           | *string*                               | :heavy_minus_sign:                     | Filter to a specific registry          |
| `search`                               | *string*                               | :heavy_minus_sign:                     | Search query to filter servers by name |
| `cursor`                               | *string*                               | :heavy_minus_sign:                     | Pagination cursor                      |
| `gramSession`                          | *string*                               | :heavy_minus_sign:                     | Session header                         |
| `gramProject`                          | *string*                               | :heavy_minus_sign:                     | project header                         |