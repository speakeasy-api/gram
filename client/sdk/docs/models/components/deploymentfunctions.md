# DeploymentFunctions

## Example Usage

```typescript
import { DeploymentFunctions } from "@gram/client/models/components/deploymentfunctions.js";

let value: DeploymentFunctions = {
  assetId: "<id>",
  id: "<id>",
  name: "<value>",
  runtime: "<value>",
  slug: "<value>",
};
```

## Fields

| Field                                                           | Type                                                            | Required                                                        | Description                                                     |
| --------------------------------------------------------------- | --------------------------------------------------------------- | --------------------------------------------------------------- | --------------------------------------------------------------- |
| `assetId`                                                       | *string*                                                        | :heavy_check_mark:                                              | The ID of the uploaded asset.                                   |
| `id`                                                            | *string*                                                        | :heavy_check_mark:                                              | The ID of the deployment asset.                                 |
| `memoryMib`                                                     | *number*                                                        | :heavy_minus_sign:                                              | The memory limit in MiB of function runner machines.            |
| `name`                                                          | *string*                                                        | :heavy_check_mark:                                              | The name to give the document as it will be displayed in UIs.   |
| `runtime`                                                       | *string*                                                        | :heavy_check_mark:                                              | The runtime to use when executing functions.                    |
| `scale`                                                         | *number*                                                        | :heavy_minus_sign:                                              | The number of instances to run for the function.                |
| `slug`                                                          | *string*                                                        | :heavy_check_mark:                                              | A short url-friendly label that uniquely identifies a resource. |