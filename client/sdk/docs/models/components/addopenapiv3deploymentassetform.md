# AddOpenAPIv3DeploymentAssetForm

## Example Usage

```typescript
import { AddOpenAPIv3DeploymentAssetForm } from "@gram/client/models/components/addopenapiv3deploymentassetform.js";

let value: AddOpenAPIv3DeploymentAssetForm = {
  assetId: "<id>",
  name: "<value>",
  slug: "<value>",
};
```

## Fields

| Field     | Type     | Required           | Description                                                     |
| --------- | -------- | ------------------ | --------------------------------------------------------------- |
| `assetId` | _string_ | :heavy_check_mark: | The ID of the uploaded asset.                                   |
| `name`    | _string_ | :heavy_check_mark: | The name to give the document as it will be displayed in UIs.   |
| `slug`    | _string_ | :heavy_check_mark: | A short url-friendly label that uniquely identifies a resource. |
