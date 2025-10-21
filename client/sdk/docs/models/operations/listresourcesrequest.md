# ListResourcesRequest

## Example Usage

```typescript
import { ListResourcesRequest } from "@gram/client/models/operations";

let value: ListResourcesRequest = {};
```

## Fields

| Field                                                        | Type                                                         | Required                                                     | Description                                                  |
| ------------------------------------------------------------ | ------------------------------------------------------------ | ------------------------------------------------------------ | ------------------------------------------------------------ |
| `cursor`                                                     | *string*                                                     | :heavy_minus_sign:                                           | The cursor to fetch results from                             |
| `limit`                                                      | *number*                                                     | :heavy_minus_sign:                                           | The number of resources to return per page                   |
| `deploymentId`                                               | *string*                                                     | :heavy_minus_sign:                                           | The deployment ID. If unset, latest deployment will be used. |
| `gramSession`                                                | *string*                                                     | :heavy_minus_sign:                                           | Session header                                               |
| `gramProject`                                                | *string*                                                     | :heavy_minus_sign:                                           | project header                                               |