# EvolveForm

## Example Usage

```typescript
import { EvolveForm } from "@gram/client/models/components";

let value: EvolveForm = {};
```

## Fields

| Field                                                                                                      | Type                                                                                                       | Required                                                                                                   | Description                                                                                                |
| ---------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------- |
| `addOpenapiv3Assets`                                                                                       | [components.AddOpenAPIv3DeploymentAssetForm](../../models/components/addopenapiv3deploymentassetform.md)[] | :heavy_minus_sign:                                                                                         | The OpenAPI 3.x documents to add to the new deployment.                                                    |
| `addPackages`                                                                                              | [components.AddPackageForm](../../models/components/addpackageform.md)[]                                   | :heavy_minus_sign:                                                                                         | The OpenAPI 3.x documents to add to the deployment.                                                        |
| `deploymentId`                                                                                             | *string*                                                                                                   | :heavy_minus_sign:                                                                                         | The ID of the deployment to evolve. If omitted, the latest deployment will be used.                        |