# ListMCPCatalogRequest

## Example Usage

```typescript
import { ListMCPCatalogRequest } from "@gram/client/models/operations";

let value: ListMCPCatalogRequest = {};
```

## Fields

| Field                                  | Type                                   | Required                               | Description                            |
| -------------------------------------- | -------------------------------------- | -------------------------------------- | -------------------------------------- |
| `registryId`                           | *string*                               | :heavy_minus_sign:                     | Filter to a specific registry          |
| `search`                               | *string*                               | :heavy_minus_sign:                     | Search query to filter servers by name |
| `cursor`                               | *string*                               | :heavy_minus_sign:                     | Pagination cursor                      |
| `gramSession`                          | *string*                               | :heavy_minus_sign:                     | Session header                         |
| `gramProject`                          | *string*                               | :heavy_minus_sign:                     | project header                         |