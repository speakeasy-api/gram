# GetDeploymentLogsRequest

## Example Usage

```typescript
import { GetDeploymentLogsRequest } from "@gram/client/models/operations/getdeploymentlogs.js";

let value: GetDeploymentLogsRequest = {
  deploymentId: "<id>",
};
```

## Fields

| Field          | Type     | Required           | Description                      |
| -------------- | -------- | ------------------ | -------------------------------- |
| `deploymentId` | _string_ | :heavy_check_mark: | The ID of the deployment         |
| `cursor`       | _string_ | :heavy_minus_sign: | The cursor to fetch results from |
| `gramKey`      | _string_ | :heavy_minus_sign: | API Key header                   |
| `gramSession`  | _string_ | :heavy_minus_sign: | Session header                   |
| `gramProject`  | _string_ | :heavy_minus_sign: | project header                   |
