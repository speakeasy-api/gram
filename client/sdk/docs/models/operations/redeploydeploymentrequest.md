# RedeployDeploymentRequest

## Example Usage

```typescript
import { RedeployDeploymentRequest } from "@gram/client/models/operations";

let value: RedeployDeploymentRequest = {
  redeployRequestBody: {
    deploymentId: "<id>",
  },
};
```

## Fields

| Field                                                                            | Type                                                                             | Required                                                                         | Description                                                                      |
| -------------------------------------------------------------------------------- | -------------------------------------------------------------------------------- | -------------------------------------------------------------------------------- | -------------------------------------------------------------------------------- |
| `gramKey`                                                                        | *string*                                                                         | :heavy_minus_sign:                                                               | API Key header                                                                   |
| `gramSession`                                                                    | *string*                                                                         | :heavy_minus_sign:                                                               | Session header                                                                   |
| `gramProject`                                                                    | *string*                                                                         | :heavy_minus_sign:                                                               | project header                                                                   |
| `redeployRequestBody`                                                            | [components.RedeployRequestBody](../../models/components/redeployrequestbody.md) | :heavy_check_mark:                                                               | N/A                                                                              |