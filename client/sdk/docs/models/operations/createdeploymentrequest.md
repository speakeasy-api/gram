# CreateDeploymentRequest

## Example Usage

```typescript
import { CreateDeploymentRequest } from "@gram/client/models/operations";

let value: CreateDeploymentRequest = {
  idempotencyKey: "01jqq0ajmb4qh9eppz48dejr2m",
};
```

## Fields

| Field                                                                                            | Type                                                                                             | Required                                                                                         | Description                                                                                      | Example                                                                                          |
| ------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------ |
| `gramKey`                                                                                        | *string*                                                                                         | :heavy_minus_sign:                                                                               | API Key header                                                                                   |                                                                                                  |
| `gramSession`                                                                                    | *string*                                                                                         | :heavy_minus_sign:                                                                               | Session header                                                                                   |                                                                                                  |
| `gramProject`                                                                                    | *string*                                                                                         | :heavy_minus_sign:                                                                               | project header                                                                                   |                                                                                                  |
| `idempotencyKey`                                                                                 | *string*                                                                                         | :heavy_check_mark:                                                                               | A unique identifier that will mitigate against duplicate deployments.                            | 01jqq0ajmb4qh9eppz48dejr2m                                                                       |
| `createDeploymentRequestBody`                                                                    | [components.CreateDeploymentRequestBody](../../models/components/createdeploymentrequestbody.md) | :heavy_check_mark:                                                                               | N/A                                                                                              |                                                                                                  |