# DeploymentSummary

## Example Usage

```typescript
import { DeploymentSummary } from "@gram/sdk/models/components";

let value: DeploymentSummary = {
  assetCount: 461479,
  createdAt: new Date("2025-05-05T11:02:22.031Z"),
  id: "bc5f4a555e933e6861d12edba4c2d87ef6caf8e6",
  userId: "<id>",
};
```

## Fields

| Field                                                                                         | Type                                                                                          | Required                                                                                      | Description                                                                                   | Example                                                                                       |
| --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `assetCount`                                                                                  | *number*                                                                                      | :heavy_check_mark:                                                                            | The number of upstream assets.                                                                |                                                                                               |
| `createdAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | The creation date of the deployment.                                                          |                                                                                               |
| `id`                                                                                          | *string*                                                                                      | :heavy_check_mark:                                                                            | The ID to of the deployment.                                                                  | bc5f4a555e933e6861d12edba4c2d87ef6caf8e6                                                      |
| `userId`                                                                                      | *string*                                                                                      | :heavy_check_mark:                                                                            | The ID of the user that created the deployment.                                               |                                                                                               |