# ListToolsRequest

## Example Usage

```typescript
import { ListToolsRequest } from "@gram/client/models/operations";

let value: ListToolsRequest = {};
```

## Fields

| Field                                                                                                    | Type                                                                                                     | Required                                                                                                 | Description                                                                                              |
| -------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------- |
| `cursor`                                                                                                 | *string*                                                                                                 | :heavy_minus_sign:                                                                                       | The cursor to fetch results from                                                                         |
| `limit`                                                                                                  | *number*                                                                                                 | :heavy_minus_sign:                                                                                       | The number of tools to return per page                                                                   |
| `deploymentId`                                                                                           | *string*                                                                                                 | :heavy_minus_sign:                                                                                       | The deployment ID. If unset, latest deployment will be used.                                             |
| `urnPrefix`                                                                                              | *string*                                                                                                 | :heavy_minus_sign:                                                                                       | Filter tools by URN prefix (e.g. 'tools:http:kitchen-sink' to match all tools starting with that prefix) |
| `gramSession`                                                                                            | *string*                                                                                                 | :heavy_minus_sign:                                                                                       | Session header                                                                                           |
| `gramProject`                                                                                            | *string*                                                                                                 | :heavy_minus_sign:                                                                                       | project header                                                                                           |