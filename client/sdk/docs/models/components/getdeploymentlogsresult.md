# GetDeploymentLogsResult

## Example Usage

```typescript
import { GetDeploymentLogsResult } from "@gram/client/models/components";

let value: GetDeploymentLogsResult = {
  events: [
    {
      createdAt: "1708702461076",
      event: "<value>",
      id: "<id>",
      message: "<value>",
    },
  ],
  status: "<value>",
};
```

## Fields

| Field                                                                            | Type                                                                             | Required                                                                         | Description                                                                      |
| -------------------------------------------------------------------------------- | -------------------------------------------------------------------------------- | -------------------------------------------------------------------------------- | -------------------------------------------------------------------------------- |
| `events`                                                                         | [components.DeploymentLogEvent](../../models/components/deploymentlogevent.md)[] | :heavy_check_mark:                                                               | The logs for the deployment                                                      |
| `nextCursor`                                                                     | *string*                                                                         | :heavy_minus_sign:                                                               | The cursor to fetch results from                                                 |
| `status`                                                                         | *string*                                                                         | :heavy_check_mark:                                                               | The status of the deployment                                                     |