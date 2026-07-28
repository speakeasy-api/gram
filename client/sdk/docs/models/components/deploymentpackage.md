# DeploymentPackage

## Example Usage

```typescript
import { DeploymentPackage } from "@gram/client/models/components/deploymentpackage.js";

let value: DeploymentPackage = {
  id: "<id>",
  name: "<value>",
  version: "<value>",
};
```

## Fields

| Field     | Type     | Required           | Description                       |
| --------- | -------- | ------------------ | --------------------------------- |
| `id`      | _string_ | :heavy_check_mark: | The ID of the deployment package. |
| `name`    | _string_ | :heavy_check_mark: | The name of the package.          |
| `version` | _string_ | :heavy_check_mark: | The version of the package.       |
