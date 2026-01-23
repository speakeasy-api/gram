# DeploymentPackage

## Example Usage

```typescript
import { DeploymentPackage } from "@gram/client/models/components";

let value: DeploymentPackage = {
  id: "<id>",
  name: "<value>",
  version: "<value>",
};
```

## Fields

| Field                             | Type                              | Required                          | Description                       |
| --------------------------------- | --------------------------------- | --------------------------------- | --------------------------------- |
| `id`                              | *string*                          | :heavy_check_mark:                | The ID of the deployment package. |
| `name`                            | *string*                          | :heavy_check_mark:                | The name of the package.          |
| `version`                         | *string*                          | :heavy_check_mark:                | The version of the package.       |