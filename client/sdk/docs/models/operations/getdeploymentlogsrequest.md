# GetDeploymentLogsRequest

## Example Usage

```typescript
import { GetDeploymentLogsRequest } from "@gram/client/models/operations";

let value: GetDeploymentLogsRequest = {
  deploymentId: "<id>",
};
```

## Fields

| Field                            | Type                             | Required                         | Description                      |
| -------------------------------- | -------------------------------- | -------------------------------- | -------------------------------- |
| `deploymentId`                   | *string*                         | :heavy_check_mark:               | The ID of the deployment         |
| `cursor`                         | *string*                         | :heavy_minus_sign:               | The cursor to fetch results from |
| `gramKey`                        | *string*                         | :heavy_minus_sign:               | API Key header                   |
| `gramSession`                    | *string*                         | :heavy_minus_sign:               | Session header                   |
| `gramProject`                    | *string*                         | :heavy_minus_sign:               | project header                   |