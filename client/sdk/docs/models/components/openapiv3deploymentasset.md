# OpenAPIv3DeploymentAsset

## Example Usage

```typescript
import { OpenAPIv3DeploymentAsset } from "@gram/client/models/components/openapiv3deploymentasset.js";

let value: OpenAPIv3DeploymentAsset = {
  assetId: "<id>",
  id: "<id>",
  name: "<value>",
  slug: "<value>",
};
```

## Fields

| Field     | Type     | Required           | Description                                                     |
| --------- | -------- | ------------------ | --------------------------------------------------------------- |
| `assetId` | _string_ | :heavy_check_mark: | The ID of the uploaded asset.                                   |
| `id`      | _string_ | :heavy_check_mark: | The ID of the deployment asset.                                 |
| `name`    | _string_ | :heavy_check_mark: | The name to give the document as it will be displayed in UIs.   |
| `slug`    | _string_ | :heavy_check_mark: | A short url-friendly label that uniquely identifies a resource. |
