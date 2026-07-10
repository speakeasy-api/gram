# ListResourcesRequest

## Example Usage

```typescript
import { ListResourcesRequest } from "@gram/client/models/operations/listresources.js";

let value: ListResourcesRequest = {};
```

## Fields

| Field          | Type     | Required           | Description                                                  |
| -------------- | -------- | ------------------ | ------------------------------------------------------------ |
| `cursor`       | _string_ | :heavy_minus_sign: | The cursor to fetch results from                             |
| `limit`        | _number_ | :heavy_minus_sign: | The number of resources to return per page                   |
| `deploymentId` | _string_ | :heavy_minus_sign: | The deployment ID. If unset, latest deployment will be used. |
| `gramSession`  | _string_ | :heavy_minus_sign: | Session header                                               |
| `gramProject`  | _string_ | :heavy_minus_sign: | project header                                               |
