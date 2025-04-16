# AddOpenAPIv3ToDeploymentRequest

## Example Usage

```typescript
import { AddOpenAPIv3ToDeploymentRequest } from "@gram/client/models/operations";

let value: AddOpenAPIv3ToDeploymentRequest = {
  openAPIv3DeploymentAssetForm: {
    assetId: "<id>",
    name: "<value>",
    slug: "<value>",
  },
};
```

## Fields

| Field                                                                                              | Type                                                                                               | Required                                                                                           | Description                                                                                        |
| -------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------- |
| `gramSession`                                                                                      | *string*                                                                                           | :heavy_minus_sign:                                                                                 | Session header                                                                                     |
| `gramProject`                                                                                      | *string*                                                                                           | :heavy_minus_sign:                                                                                 | project header                                                                                     |
| `openAPIv3DeploymentAssetForm`                                                                     | [components.OpenAPIv3DeploymentAssetForm](../../models/components/openapiv3deploymentassetform.md) | :heavy_check_mark:                                                                                 | N/A                                                                                                |