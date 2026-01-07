# OpenAPIv3DeploymentAsset

## Example Usage

```typescript
import { OpenAPIv3DeploymentAsset } from "@gram/client/models/components";

let value: OpenAPIv3DeploymentAsset = {
  assetId: "<id>",
  id: "<id>",
  name: "<value>",
  slug: "<value>",
};
```

## Fields

| Field                                                           | Type                                                            | Required                                                        | Description                                                     |
| --------------------------------------------------------------- | --------------------------------------------------------------- | --------------------------------------------------------------- | --------------------------------------------------------------- |
| `assetId`                                                       | *string*                                                        | :heavy_check_mark:                                              | The ID of the uploaded asset.                                   |
| `id`                                                            | *string*                                                        | :heavy_check_mark:                                              | The ID of the deployment asset.                                 |
| `name`                                                          | *string*                                                        | :heavy_check_mark:                                              | The name to give the document as it will be displayed in UIs.   |
| `slug`                                                          | *string*                                                        | :heavy_check_mark:                                              | A short url-friendly label that uniquely identifies a resource. |