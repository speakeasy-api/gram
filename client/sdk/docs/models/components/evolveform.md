# EvolveForm

## Example Usage

```typescript
import { EvolveForm } from "@gram/client/models/components";

let value: EvolveForm = {};
```

## Fields

| Field                                                                                                      | Type                                                                                                       | Required                                                                                                   | Description                                                                                                |
| ---------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------- |
| `deploymentId`                                                                                             | *string*                                                                                                   | :heavy_minus_sign:                                                                                         | The ID of the deployment to evolve. If omitted, the latest deployment will be used.                        |
| `upsertOpenapiv3Assets`                                                                                    | [components.AddOpenAPIv3DeploymentAssetForm](../../models/components/addopenapiv3deploymentassetform.md)[] | :heavy_minus_sign:                                                                                         | The OpenAPI 3.x documents to upsert in the new deployment.                                                 |
| `upsertPackages`                                                                                           | [components.AddPackageForm](../../models/components/addpackageform.md)[]                                   | :heavy_minus_sign:                                                                                         | The packages to upsert in the new deployment.                                                              |